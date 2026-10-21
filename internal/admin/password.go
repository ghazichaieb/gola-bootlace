// Passwords of the web interface: kept as a salted PBKDF2 hash in the settings file, never the password itself.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package admin

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

const iterations = 210000

// HashPassword returns "pbkdf2-sha256$iterations$salt$hash".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword compares a password with a stored hash. An empty or damaged hash never matches.
func CheckPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1000 || n > 5_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, n, len(want))
	return err == nil && equal(string(got), string(want))
}

// readable letters and digits without the ones that look alike
const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomPassword makes a password a person can type from the log.
func RandomPassword() string {
	b := make([]byte, 14)
	_, _ = rand.Read(b)
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out)
}

// EnsurePassword makes sure the interface has a password. A password given on the command line (or in the environment) becomes the password when it
// differs from the stored one. Without one, a new password is made the first time; it is returned so that the caller can show it once.
func EnsurePassword(store *config.Store, given string) (generated string, err error) {
	c := store.Get()
	switch {
	case given != "":
		if CheckPassword(c.AdminHash, given) {
			return "", nil
		}
		h, err := HashPassword(given)
		if err != nil {
			return "", err
		}
		return "", store.Update(func(c *config.Config) error { c.AdminHash = h; return nil })
	case c.AdminHash == "":
		pw := RandomPassword()
		h, err := HashPassword(pw)
		if err != nil {
			return "", err
		}
		return pw, store.Update(func(c *config.Config) error { c.AdminHash = h; return nil })
	}
	return "", nil
}
