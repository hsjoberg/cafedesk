package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Staff is someone who works the till.
type Staff struct {
	ID            string
	Name          string
	Role          string // roleCashier or roleManager
	Active        bool
	MustChangePIN bool
}

const (
	roleCashier = "cashier"
	roleManager = "manager"

	pinLength  = 4
	defaultPIN = "1234"
)

// IsManager reports whether the person may approve voids, refunds and
// closing the day, and change settings.
func (s Staff) IsManager() bool { return s.Role == roleManager }

// A PIN of four digits has 10,000 values, so no hash keeps a stolen
// database from revealing it; the hash keeps PINs from being read off
// the disk at a glance, and the till limits attempts. Argon2id with small
// costs keeps checking every PIN quick on an old POS PC.
func hashPIN(pin string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey([]byte(pin), salt, 1, 8*1024, 1, 32)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(sum)
}

func checkPIN(hash, pin string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, 1, 8*1024, 1, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validPIN(pin string) bool {
	if len(pin) != pinLength {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var (
	errPINTaken   = errors.New("someone else already has that PIN")
	errPINInvalid = errors.New("a PIN is 4 digits")
)

// EnsureStaff adds a manager with the default PIN, which must be changed
// at first sign-in, when there is no one yet.
func (s *Store) EnsureStaff(ctx context.Context) error {
	rows, err := s.query(ctx, "SELECT COUNT(*) FROM staff")
	if err != nil {
		return err
	}
	if firstInt(rows) > 0 {
		return nil
	}
	_, err = s.exec(ctx, `INSERT INTO staff (id, name, role, pin_hash, active, must_change_pin, created_at)
		VALUES (?, 'Owner', ?, ?, 1, 1, ?)`, newID(), roleManager, hashPIN(defaultPIN), ts(time.Now()))
	return err
}

// AllStaff lists everyone, active first.
func (s *Store) AllStaff(ctx context.Context) ([]Staff, error) {
	rows, err := s.query(ctx, `SELECT id, name, role, active, must_change_pin FROM staff
		ORDER BY active DESC, name`)
	if err != nil {
		return nil, err
	}
	out := make([]Staff, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, Staff{ID: str(r[0]), Name: str(r[1]), Role: str(r[2]),
			Active: i64(r[3]) != 0, MustChangePIN: i64(r[4]) != 0})
	}
	return out, nil
}

// StaffByPIN returns the active person with that PIN.
func (s *Store) StaffByPIN(ctx context.Context, pin string) (Staff, bool, error) {
	rows, err := s.query(ctx, `SELECT id, name, role, must_change_pin, pin_hash FROM staff WHERE active = 1`)
	if err != nil {
		return Staff{}, false, err
	}
	for _, r := range rows.Values {
		if checkPIN(str(r[4]), pin) {
			return Staff{ID: str(r[0]), Name: str(r[1]), Role: str(r[2]), Active: true,
				MustChangePIN: i64(r[3]) != 0}, true, nil
		}
	}
	return Staff{}, false, nil
}

// pinTaken reports whether an active person other than id has the PIN.
func (s *Store) pinTaken(ctx context.Context, pin, id string) (bool, error) {
	other, ok, err := s.StaffByPIN(ctx, pin)
	return ok && other.ID != id, err
}

// AddStaff adds a person.
func (s *Store) AddStaff(ctx context.Context, name, role, pin string) (Staff, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return Staff{}, errors.New("enter a name")
	case !validPIN(pin):
		return Staff{}, errPINInvalid
	case pin == defaultPIN:
		return Staff{}, errors.New("choose a PIN other than 1234")
	}
	taken, err := s.pinTaken(ctx, pin, "")
	if err != nil {
		return Staff{}, err
	}
	if taken {
		return Staff{}, errPINTaken
	}
	st := Staff{ID: newID(), Name: name, Role: role, Active: true}
	_, err = s.exec(ctx, `INSERT INTO staff (id, name, role, pin_hash, active, must_change_pin, created_at)
		VALUES (?, ?, ?, ?, 1, 0, ?)`, st.ID, st.Name, st.Role, hashPIN(pin), ts(time.Now()))
	return st, err
}

// SetPIN changes a person's PIN.
func (s *Store) SetPIN(ctx context.Context, id, pin string) error {
	if !validPIN(pin) {
		return errPINInvalid
	}
	if pin == defaultPIN {
		return errors.New("choose a PIN other than 1234")
	}
	if taken, err := s.pinTaken(ctx, pin, id); err != nil {
		return err
	} else if taken {
		return errPINTaken
	}
	_, err := s.exec(ctx, "UPDATE staff SET pin_hash = ?, must_change_pin = 0 WHERE id = ?", hashPIN(pin), id)
	return err
}

// UpdateStaff changes a person's name, role and whether they may sign in.
// The last active manager can't be demoted or switched off.
func (s *Store) UpdateStaff(ctx context.Context, st Staff) error {
	if st.Role != roleManager || !st.Active {
		rows, err := s.query(ctx, `SELECT COUNT(*) FROM staff WHERE role = ? AND active = 1 AND id != ?`,
			roleManager, st.ID)
		if err != nil {
			return err
		}
		if firstInt(rows) == 0 {
			return errors.New("the till needs at least one active manager")
		}
	}
	_, err := s.exec(ctx, "UPDATE staff SET name = ?, role = ?, active = ? WHERE id = ?",
		strings.TrimSpace(st.Name), st.Role, b2i(st.Active), st.ID)
	return err
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
