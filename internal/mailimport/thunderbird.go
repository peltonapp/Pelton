package mailimport

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Thunderbird keeps everything about an account in prefs.js inside its profile
// directory: a flat key/value file of user_pref() calls, cross-referenced by
// opaque ids (account1 -> server1 -> imap.example.com). Mail itself sits next
// to it as mbox files. Nothing here writes to the profile, and no password is
// read: Thunderbird's are in its own credential store, and the user
// re-authenticates once after importing.

// Profile is one discovered Thunderbird profile.
type Profile struct {
	// Name is the profile directory's name, which is what Thunderbird shows in
	// its profile manager (for example "abc123.default-release").
	Name string
	Path string
	// Accounts are the mail accounts configured in this profile.
	Accounts []Account
	// LocalFolders are the mbox files of the profile's Local Folders store,
	// the mail that lives only on this machine.
	LocalFolders []MboxFile
}

// Account is one mail account read out of a profile. Password is deliberately
// absent: it is never read, and the user re-enters it once after importing.
type Account struct {
	Email       string
	DisplayName string
	// Username is the login name when it differs from the address.
	Username string
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
	// Kind is "imap" or "pop3". Pelton speaks imap only, so a pop3 account is
	// reported for the ui to explain rather than silently dropped.
	Kind string
}

// MboxFile is one mail folder on disk, with the display name it should keep.
type MboxFile struct {
	// Name is the folder name including its parents, joined with "/" for
	// nested folders (Thunderbird stores those in .sbd directories).
	Name string
	Path string
	// SizeBytes is the file size, so the ui can warn before a long import.
	SizeBytes int64
}

// DiscoverThunderbird finds every Thunderbird profile on this machine and
// reads its accounts and local mail folders. A profile that cannot be read is
// skipped rather than failing the whole scan, since one broken profile should
// not hide the others. It returns an empty slice when Thunderbird is not
// installed.
func DiscoverThunderbird() ([]Profile, error) {
	var profiles []Profile
	for _, dir := range profileDirs() {
		profile, err := readProfile(dir)
		if err != nil {
			continue
		}
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// ReadProfile reads one profile directory the user picked by hand, for the
// case where their profile lives somewhere discovery does not look.
func ReadProfile(dir string) (Profile, error) {
	return readProfile(dir)
}

// profileDirs returns every directory that looks like a Thunderbird profile.
// A profile is a directory holding prefs.js, either directly under a root or
// one level down in Profiles/.
func profileDirs() []string {
	var dirs []string
	for _, root := range thunderbirdRoots() {
		if hasPrefs(root) {
			dirs = append(dirs, root)
		}
		for _, parent := range []string{root, filepath.Join(root, "Profiles")} {
			entries, err := os.ReadDir(parent)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				candidate := filepath.Join(parent, e.Name())
				if hasPrefs(candidate) {
					dirs = append(dirs, candidate)
				}
			}
		}
	}
	return unique(dirs)
}

// thunderbirdRoots lists the per-platform locations Thunderbird keeps profiles
// in. Linux gets several because the distro package, the Flatpak and the Snap
// each sandbox their profile somewhere different, and a Fedora machine can
// have both the rpm and the Flatpak installed at once.
func thunderbirdRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Thunderbird"),
			filepath.Join(home, "Library", "Application Support", "Thunderbird"),
		}
	case "windows":
		var roots []string
		if appData := os.Getenv("APPDATA"); appData != "" {
			roots = append(roots, filepath.Join(appData, "Thunderbird"))
		}
		return append(roots, filepath.Join(home, "AppData", "Roaming", "Thunderbird"))
	default:
		return []string{
			filepath.Join(home, ".thunderbird"),
			filepath.Join(home, ".mozilla-thunderbird"),
			filepath.Join(home, ".var", "app", "org.mozilla.Thunderbird", ".thunderbird"),
			filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
		}
	}
}

func hasPrefs(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "prefs.js"))
	return err == nil && !info.IsDir()
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func readProfile(dir string) (Profile, error) {
	prefs, err := readPrefs(filepath.Join(dir, "prefs.js"))
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		Name:         filepath.Base(dir),
		Path:         dir,
		Accounts:     accountsFromPrefs(prefs),
		LocalFolders: localFolders(dir, prefs),
	}, nil
}

// prefs is a parsed prefs.js: every user_pref key mapped to its value, with
// quoted strings unquoted and numbers left as their text.
type prefs map[string]string

func readPrefs(path string) (prefs, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("mailimport: open prefs.js: %w", err)
	}
	defer f.Close()

	out := make(prefs)
	scanner := bufio.NewScanner(f)
	// prefs.js lines are short, but a stray long one should not abort the scan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		key, value, ok := parsePrefLine(scanner.Text())
		if ok {
			out[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mailimport: read prefs.js: %w", err)
	}
	return out, nil
}

// parsePrefLine reads one `user_pref("key", value);` line. Anything else (the
// file's header comment, blank lines) is skipped.
func parsePrefLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	const prefix = `user_pref("`
	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}
	rest := line[len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", "", false
	}
	key = rest[:end]

	rest = strings.TrimSpace(rest[end+1:])
	rest = strings.TrimPrefix(rest, ",")
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, ")")
	rest = strings.TrimSpace(rest)
	return key, unquotePref(rest), true
}

// unquotePref strips the quotes from a string value and undoes the two escapes
// Thunderbird writes (\" and \\). Numbers and booleans are returned as text.
func unquotePref(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}
	inner := value[1 : len(value)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			b.WriteByte(inner[i])
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// accountsFromPrefs resolves the account -> server -> identity -> smtp chain
// into flat account records. Accounts listed but missing their server entry
// (Thunderbird leaves those behind after a removal) are skipped.
func accountsFromPrefs(p prefs) []Account {
	var out []Account
	for _, id := range splitList(p["mail.accountmanager.accounts"]) {
		serverKey := p["mail.account."+id+".server"]
		if serverKey == "" {
			continue
		}
		kind := p["mail.server."+serverKey+".type"]
		if kind != "imap" && kind != "pop3" {
			// "none" is the Local Folders pseudo-account, which has no server.
			continue
		}

		account := Account{
			Kind:     kind,
			IMAPHost: p["mail.server."+serverKey+".hostname"],
			IMAPPort: atoi(p["mail.server."+serverKey+".port"]),
			Username: p["mail.server."+serverKey+".userName"],
		}

		identities := splitList(p["mail.account."+id+".identities"])
		if len(identities) > 0 {
			identity := identities[0]
			account.Email = p["mail.identity."+identity+".useremail"]
			account.DisplayName = p["mail.identity."+identity+".fullName"]
			smtpKey := p["mail.identity."+identity+".smtpServer"]
			if smtpKey == "" {
				smtpKey = p["mail.smtp.defaultserver"]
			}
			if smtpKey != "" {
				account.SMTPHost = p["mail.smtpserver."+smtpKey+".hostname"]
				account.SMTPPort = atoi(p["mail.smtpserver."+smtpKey+".port"])
			}
		}
		if account.Email == "" {
			// without an address there is nothing to create an account under;
			// the server name is the closest thing Thunderbird records.
			account.Email = p["mail.server."+serverKey+".name"]
		}
		if account.Email == "" || account.IMAPHost == "" {
			continue
		}
		// the login name matches the address often enough that storing it
		// again would just be noise in the account row.
		if account.Username == account.Email {
			account.Username = ""
		}
		out = append(out, account)
	}
	return out
}

// localFolders finds the profile's Local Folders store and lists the mbox
// files in it. The store's location is a pref, either absolute or relative to
// the profile directory; the conventional Mail/Local Folders path is the
// fallback when neither pref is set.
func localFolders(profileDir string, p prefs) []MboxFile {
	dir := localFoldersDir(profileDir, p)
	if dir == "" {
		return nil
	}
	return scanMailDir(dir, "")
}

func localFoldersDir(profileDir string, p prefs) string {
	for key, value := range p {
		if !strings.HasSuffix(key, ".type") || value != "none" {
			continue
		}
		server := strings.TrimSuffix(strings.TrimPrefix(key, "mail.server."), ".type")
		if dir := p["mail.server."+server+".directory"]; dir != "" {
			return dir
		}
		if rel := p["mail.server."+server+".directory-rel"]; rel != "" {
			return filepath.Join(profileDir, filepath.FromSlash(strings.TrimPrefix(rel, "[ProfD]")))
		}
	}
	fallback := filepath.Join(profileDir, "Mail", "Local Folders")
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return fallback
	}
	return ""
}

// scanMailDir lists the mbox files in a Thunderbird mail directory, recursing
// into the .sbd directories that hold nested folders. Index files (.msf) and
// the other sidecars Thunderbird writes are skipped: an mbox has no extension.
func scanMailDir(dir, prefix string) []MboxFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []MboxFile
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() {
			if before, ok := strings.CutSuffix(name, ".sbd"); ok {
				out = append(out, scanMailDir(path, join(prefix, before))...)
			}
			continue
		}
		if filepath.Ext(name) != "" || strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		out = append(out, MboxFile{Name: join(prefix, name), Path: path, SizeBytes: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func splitList(value string) []string {
	var out []string
	for part := range strings.SplitSeq(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func atoi(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}
