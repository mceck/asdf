// Package resolve contains functions for resolving a tool version in a given
// directory. This is a core feature of asdf as asdf must be able to resolve a
// tool version in any directory if set.
package resolve

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/asdf-vm/asdf/internal/config"
	"github.com/asdf-vm/asdf/internal/installs"
	"github.com/asdf-vm/asdf/internal/plugins"
	"github.com/asdf-vm/asdf/internal/toolversions"
)

// ToolVersions represents a tool along with versions specified for it
type ToolVersions struct {
	Versions  []string
	Directory string
	Source    string
}

// Version takes a plugin and a directory and resolves the tool to one or more
// versions.
func Version(conf config.Config, plugin plugins.Plugin, directory string) (versions ToolVersions, found bool, err error) {
	version, envVariableName, found := findVersionsInEnv(plugin.Name)
	if found {
		return ToolVersions{Versions: version, Source: envVariableName}, true, nil
	}

	for !found {
		versions, found, err = findVersionsInDir(conf, plugin, directory)
		if err != nil {
			return versions, false, err
		}

		nextDir := path.Dir(directory)
		// If current dir and next dir are the same it means we've reached `/` and
		// have no more parent directories to search.
		if nextDir == directory {
			// If no version found, try current users home directory. I'd like to
			// eventually remove this feature.
			homeDir, osErr := os.UserHomeDir()
			if osErr != nil {
				break
			}

			versions, found, err = findVersionsInDir(conf, plugin, homeDir)
			break
		}
		directory = nextDir
	}

	return versions, found, err
}

// FindBestMatchingVersion returns the best matching version for a plugin based on
// the installed versions and the versions specified in the plugin's configuration.
// It considers the environment variables ASDF_IGNORE_PATCH, ASDF_IGNORE_MINOR, ASDF_IGNORE_VERSION
// These variables allow users to ignore .tool-versions constraints.
// The best matching version is determined by the following rules:
// If ASDF_IGNORE_VERSION is set, returns always the latest installed version of the plugin.
// If ASDF_IGNORE_PATCH is set, returns the latest installed version that matches the major.minor version.
// If ASDF_IGNORE_MINOR is set, returns the latest installed version that matches the major version.
// You can set these environment variables to "*" to use the ignore rule for all plugins.
// Example:
// ASDF_IGNORE_PATCH=* # ignores all patch versions
// ASDF_IGNORE_MINOR=nodejs golang # ignores all minor/patch versions for nodejs and golang
func FindBestMatchingVersion(conf config.Config, plugin plugins.Plugin, versions []string) string {
	availableVersions, err := installs.Installed(conf, plugin)
	if err != nil {
		return ""
	}
	ignorePatches := strings.Split(os.Getenv("ASDF_IGNORE_PATCH"), " ")
	ignoreMinors := strings.Split(os.Getenv("ASDF_IGNORE_MINOR"), " ")
	ignoreVersions := strings.Split(os.Getenv("ASDF_IGNORE_VERSION"), " ")
	slices.SortFunc(availableVersions, func(a, b string) int { return -strings.Compare(a, b) })
	if slices.Contains(ignoreVersions, plugin.Name) || slices.Contains(ignoreVersions, "*") {
		return availableVersions[0]
	}
	if len(ignorePatches) == 0 && len(ignoreMinors) == 0 {
		return ""
	}
	slices.SortFunc(versions, func(a, b string) int { return -strings.Compare(a, b) })
	for _, version := range availableVersions {
		if slices.Contains(ignorePatches, plugin.Name) || slices.Contains(ignorePatches, "*") {
			majorMinor := strings.Join(strings.Split(version, ".")[:2], ".")
			for _, v := range versions {
				if strings.HasPrefix(v, majorMinor) {
					return version
				}
			}
		}
		if slices.Contains(ignoreMinors, plugin.Name) || slices.Contains(ignoreMinors, "*") {
			major := strings.Split(version, ".")[0]
			for _, v := range versions {
				if strings.HasPrefix(v, major) {
					return version
				}
			}
		}
	}
	return ""
}

func findVersionsInDir(conf config.Config, plugin plugins.Plugin, directory string) (versions ToolVersions, found bool, err error) {
	filepath := path.Join(directory, conf.DefaultToolVersionsFilename)

	if _, err = os.Stat(filepath); err == nil {
		versions, found, err := toolversions.FindToolVersions(filepath, plugin.Name)
		if found || err != nil {
			return ToolVersions{Versions: versions, Source: conf.DefaultToolVersionsFilename, Directory: directory}, found, err
		}
	}

	legacyFiles, err := conf.LegacyVersionFile()
	if err != nil {
		return versions, found, err
	}

	if legacyFiles {
		versions, found, err := findVersionsInLegacyFile(plugin, directory)

		if found || err != nil {
			return versions, found, err
		}
	}

	return versions, found, nil
}

// findVersionsInEnv returns the version from the environment if present
func findVersionsInEnv(pluginName string) ([]string, string, bool) {
	envVariableName := variableVersionName(pluginName)
	versionString := os.Getenv(envVariableName)
	if versionString == "" {
		return []string{}, envVariableName, false
	}
	return parseVersion(versionString), envVariableName, true
}

// findVersionsInLegacyFile looks up a legacy version in the given directory if
// the specified plugin has a list-legacy-filenames callback script. If the
// callback script exists asdf will look for files with the given name in the
// current and extract the version from them.
func findVersionsInLegacyFile(plugin plugins.Plugin, directory string) (versions ToolVersions, found bool, err error) {
	var legacyFileNames []string

	legacyFileNames, err = plugin.LegacyFilenames()
	if err != nil {
		return versions, false, err
	}

	for _, filename := range legacyFileNames {
		filepath := path.Join(directory, filename)
		if _, err := os.Stat(filepath); err == nil {
			versionsSlice, err := plugin.ParseLegacyVersionFile(filepath)

			if len(versionsSlice) == 0 || (len(versionsSlice) == 1 && versionsSlice[0] == "") {
				return versions, false, nil
			}
			return ToolVersions{Versions: versionsSlice, Source: filename, Directory: directory}, err == nil, err
		}
	}

	return versions, found, err
}

// parseVersion parses the raw version
func parseVersion(rawVersions string) []string {
	var versions []string
	for _, version := range strings.Split(rawVersions, " ") {
		version = strings.TrimSpace(version)
		if len(version) > 0 {
			versions = append(versions, version)
		}
	}
	return versions
}

func variableVersionName(toolName string) string {
	return fmt.Sprintf("ASDF_%s_VERSION", strings.ToUpper(toolName))
}
