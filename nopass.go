package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinglinwen/goto/ssh"
)

func setupNopass(host, user, port, pass, keypath, alias string, force bool, jumpHost *ssh.JumpHost, proxyCommand string) error {
	entry := sshConfigEntry{
		Alias:        alias,
		HostName:     host,
		User:         user,
		Port:         port,
		IdentityFile: expandHomePath(keypath),
	}
	mainConfigPath := defaultSSHConfigPath()
	generatedConfigPath := defaultGeneratedSSHConfigPath(alias)
	if err := validateGeneratedSSHConfigEntry(mainConfigPath, generatedConfigPath, entry, force); err != nil {
		return err
	}
	publicKey, err := ssh.PublicKeyForPrivateKey(keypath)
	if err != nil {
		return fmt.Errorf("read public key from %s: %w", keypath, err)
	}
	options := []ssh.Option{
		ssh.SetPort(port),
		ssh.SetKeyPath(keypath),
	}
	if jumpHost != nil {
		options = append(options, ssh.SetJumpHost(*jumpHost))
	}
	if proxyCommand != "" {
		options = append(options, ssh.SetProxyCommand(proxyCommand))
	}
	t := ssh.New(host, user, pass, options...)
	if err := t.Run(buildAuthorizedKeysCommand(publicKey)); err != nil {
		_ = t.Close()
		return err
	}
	if err := t.Close(); err != nil {
		return err
	}
	return upsertGeneratedSSHConfigEntry(mainConfigPath, generatedConfigPath, entry, force)
}

func buildAuthorizedKeysCommand(publicKey string) string {
	quotedKey := shellQuote(strings.TrimSpace(publicKey))
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && " +
		"grep -qxF " + quotedKey + " ~/.ssh/authorized_keys || printf '%s\\n' " + quotedKey + " >> ~/.ssh/authorized_keys"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func nopassAlias(target string) string {
	_, host, _ := parseHost(target, "", "")
	if host != "" {
		return host
	}
	return target
}

type sshConfigEntry struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

func upsertGeneratedSSHConfigEntry(mainPath, generatedPath string, entry sshConfigEntry, force bool) error {
	if err := validateGeneratedSSHConfigEntry(mainPath, generatedPath, entry, force); err != nil {
		return err
	}
	if err := ensureSSHConfigInclude(mainPath, "config.d/*"); err != nil {
		return err
	}
	return upsertSSHConfigEntry(generatedPath, entry, force)
}

func validateGeneratedSSHConfigEntry(mainPath, generatedPath string, entry sshConfigEntry, force bool) error {
	if pathHasExactSSHConfigHost(mainPath, entry.Alias) {
		return fmt.Errorf("Host %q already exists in %s; edit it manually or choose another target", entry.Alias, mainPath)
	}
	if existingPath, found := findExactSSHConfigHostInIncludedFiles(filepath.Dir(generatedPath), filepath.Base(generatedPath), entry.Alias); found && existingPath != generatedPath {
		return fmt.Errorf("Host %q already exists in %s; edit it manually or choose another target", entry.Alias, existingPath)
	}
	if pathHasExactSSHConfigHost(generatedPath, entry.Alias) && !force {
		return fmt.Errorf("Host %q already exists in %s; use -force to replace it", entry.Alias, generatedPath)
	}
	return nil
}

func ensureSSHConfigInclude(path, include string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return os.WriteFile(path, []byte("Include "+include+"\n\n"), 0600)
	}
	if err != nil {
		return err
	}
	if sshConfigHasInclude(string(content), include) {
		return nil
	}
	updated := "Include " + include + "\n\n" + string(content)
	return os.WriteFile(path, []byte(updated), 0600)
}

func sshConfigHasInclude(content, include string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		key, value, ok := splitSSHConfigLine(stripSSHConfigComment(strings.TrimSpace(line)))
		if ok && strings.EqualFold(key, "include") && sshConfigIncludeHasPattern(value, include) {
			return true
		}
	}
	return false
}

func sshConfigIncludeHasPattern(value, include string) bool {
	for _, field := range strings.Fields(value) {
		if field == include {
			return true
		}
	}
	return false
}

func findExactSSHConfigHostInIncludedFiles(dir, generatedFile, alias string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	generatedPath := filepath.Join(dir, generatedFile)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if path == generatedPath {
			continue
		}
		if pathHasExactSSHConfigHost(path, alias) {
			return path, true
		}
	}
	return "", false
}

func pathHasExactSSHConfigHost(path, alias string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, _, found := findSSHConfigHostBlock(string(content), alias)
	return found
}

func upsertSSHConfigEntry(path string, entry sshConfigEntry, force bool) error {
	if entry.Alias == "" {
		return fmt.Errorf("empty ssh config alias")
	}
	if entry.HostName == "" {
		return fmt.Errorf("empty ssh config hostname")
	}
	if entry.User == "" {
		return fmt.Errorf("empty ssh config user")
	}
	if entry.Port == "" {
		entry.Port = "22"
	}
	if entry.IdentityFile == "" {
		entry.IdentityFile = defaultKeyPath()
	}

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	replacement := renderSSHConfigEntry(entry)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(replacement), 0600)
	}

	start, end, found := findSSHConfigHostBlock(string(content), entry.Alias)
	if found && !force {
		return fmt.Errorf("Host %q already exists in %s; use -force to replace it", entry.Alias, path)
	}
	if found {
		updated := string(content[:start]) + replacement + string(content[end:])
		return os.WriteFile(path, []byte(updated), 0600)
	}

	updated := string(content)
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	if updated != "" && !strings.HasSuffix(updated, "\n\n") {
		updated += "\n"
	}
	updated += replacement
	return os.WriteFile(path, []byte(updated), 0600)
}

func renderSSHConfigEntry(entry sshConfigEntry) string {
	return fmt.Sprintf("Host %s\n  HostName %s\n  User %s\n  Port %s\n  IdentityFile %s\n\n",
		entry.Alias, entry.HostName, entry.User, entry.Port, entry.IdentityFile)
}

func findSSHConfigHostBlock(content, alias string) (start, end int, found bool) {
	lines := strings.SplitAfter(content, "\n")
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		key, value, ok := splitSSHConfigLine(stripSSHConfigComment(trimmed))
		if !ok || !strings.EqualFold(key, "host") || !hostLineHasAlias(value, alias) {
			offset += len(line)
			continue
		}
		start = offset
		end = len(content)
		nextOffset := offset + len(line)
		for _, nextLine := range lines[i+1:] {
			nextTrimmed := strings.TrimSpace(nextLine)
			nextKey, _, nextOK := splitSSHConfigLine(stripSSHConfigComment(nextTrimmed))
			if nextOK && (strings.EqualFold(nextKey, "host") || strings.EqualFold(nextKey, "match")) {
				end = nextOffset
				break
			}
			nextOffset += len(nextLine)
		}
		return start, end, true
	}
	return 0, 0, false
}

func hostLineHasAlias(value, alias string) bool {
	for _, field := range strings.Fields(value) {
		if strings.EqualFold(field, alias) {
			return true
		}
	}
	return false
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

func defaultSSHConfigPath() string {
	return filepath.Join(homedir(), ".ssh/config")
}

func defaultGeneratedSSHConfigPath(alias string) string {
	return filepath.Join(homedir(), ".ssh/config.d", sshConfigFileName(alias))
}

func sshConfigFileName(alias string) string {
	var b strings.Builder
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), "._-")
	if name == "" {
		return "host"
	}
	return name
}

func expandHomePath(path string) string {
	if path == "~" {
		return homedir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homedir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}
