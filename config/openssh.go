package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type SSHConfigHost struct {
	Found        bool
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyCommand string
}

func GetSSHConfigHost(expr string) SSHConfigHost {
	home, _ := os.UserHomeDir()
	if home == "" {
		return SSHConfigHost{}
	}
	return parseSSHConfigHost(filepath.Join(home, ".ssh", "config"), expr)
}

func parseSSHConfigHost(path, expr string) SSHConfigHost {
	file, err := os.Open(path)
	if err != nil {
		return SSHConfigHost{}
	}
	defer file.Close()

	var got SSHConfigHost
	patterns := []string{"*"}
	matched := matchHostPatterns(patterns, expr)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripSSHConfigComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		key, value, ok := splitSSHConfigLine(line)
		if !ok {
			continue
		}
		key = strings.ToLower(key)
		if key == "host" {
			patterns = strings.Fields(value)
			matched = matchHostPatterns(patterns, expr)
			continue
		}
		if !matched {
			continue
		}
		switch key {
		case "hostname":
			if got.HostName == "" {
				got.HostName = value
				got.Found = true
			}
		case "user":
			if got.User == "" {
				got.User = value
				got.Found = true
			}
		case "port":
			if got.Port == "" {
				got.Port = value
				got.Found = true
			}
		case "identityfile":
			if got.IdentityFile == "" {
				got.IdentityFile = value
				got.Found = true
			}
		case "proxycommand":
			if got.ProxyCommand == "" {
				got.ProxyCommand = value
				got.Found = true
			}
		}
	}
	return got
}

func splitSSHConfigLine(line string) (key, value string, ok bool) {
	if idx := strings.Index(line, "="); idx >= 0 {
		key = strings.TrimSpace(line[:idx])
		value = strings.TrimSpace(line[idx+1:])
		return key, value, key != "" && value != ""
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.TrimSpace(line[len(fields[0]):]), true
}

func stripSSHConfigComment(line string) string {
	if strings.HasPrefix(line, "#") {
		return ""
	}
	if idx := strings.Index(line, " #"); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	return line
}

func matchHostPatterns(patterns []string, expr string) bool {
	if len(patterns) == 0 {
		return false
	}
	expr = strings.ToLower(expr)
	matched := false
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		ok, err := filepath.Match(strings.ToLower(pattern), expr)
		if err != nil {
			ok = strings.EqualFold(pattern, expr)
		}
		if !ok {
			continue
		}
		if negated {
			return false
		}
		matched = true
	}
	return matched
}
