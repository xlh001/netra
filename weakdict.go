package main

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"
)

//go:embed data/weak_passwords.txt
var defaultWeakPasswordDictRaw string

func defaultWeakPasswordDict() []string {
	lines := strings.Split(defaultWeakPasswordDictRaw, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

type weakPasswordDict struct {
	mu  sync.RWMutex
	set map[string]bool
}

func newWeakPasswordDict() *weakPasswordDict {
	return &weakPasswordDict{set: map[string]bool{}}
}

func (d *weakPasswordDict) rebuild(entries []string) {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e != "" {
			set[e] = true
		}
	}
	d.mu.Lock()
	d.set = set
	d.mu.Unlock()
}

func (d *weakPasswordDict) checkWithUsername(username, password string) (string, bool) {
	if password == "" {
		return "", false
	}
	if username != "" && strings.EqualFold(username, password) {
		return "same_as_username", true
	}

	d.mu.RLock()
	_, dictHit := d.set[password]
	d.mu.RUnlock()
	if dictHit {
		return "matched_dictionary", true
	}

	return checkStructuralWeakness(password)
}

var sequentialPatterns = []string{
	"12345678", "123456789", "1234567890", "87654321",
	"qwertyui", "qwertyuiop", "asdfghjk", "zxcvbnm",
	"abcdefgh", "password", "iloveyou",
}

func checkStructuralWeakness(password string) (string, bool) {
	if len([]rune(password)) < 8 {
		return "too_short", true
	}

	allDigits, allLower := true, true
	for _, r := range password {
		if !unicode.IsDigit(r) {
			allDigits = false
		}
		if !unicode.IsLower(r) {
			allLower = false
		}
	}
	if allDigits {
		return "all_digits", true
	}
	if allLower {
		return "all_lowercase", true
	}

	lower := strings.ToLower(password)
	for _, p := range sequentialPatterns {
		if strings.Contains(lower, p) {
			return "sequential_pattern", true
		}
	}

	return "", false
}
