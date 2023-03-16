package xunix

import (
	"bufio"
	"io"
	"os/user"
	"strings"

	"golang.org/x/xerrors"
)

// User is a linux user from /etc/passwd.
// It is basically a user.User with addition of the
// user's shell.
type User struct {
	user.User
	Shell string
}

// ParsePasswd parses user entries from an /etc/passwd.
func ParsePasswd(r io.Reader) ([]*User, error) {
	var (
		scanner = bufio.NewScanner(r)
		users   = make([]*User, 0)
	)

	for scanner.Scan() {
		usr, err := parsePasswdEntry(scanner.Text())
		if err != nil {
			return nil, xerrors.Errorf("failed to parse user entry: %w", err)
		}

		users = append(users, usr)
	}

	err := scanner.Err()
	if err != nil {
		return nil, xerrors.Errorf("failed to parse passwd: %w", err)
	}

	return users, nil
}

func parsePasswdEntry(entry string) (*User, error) {
	entry = strings.TrimSpace(entry)
	fields := strings.Split(entry, ":")
	if len(fields) < 7 {
		return nil, xerrors.Errorf("user info (%s) contained an unexpected number of fields", fields)
	}

	return &User{
		User: user.User{
			Username: fields[0],
			Uid:      fields[2],
			Gid:      fields[3],
			HomeDir:  fields[5],
		},
		Shell: fields[6],
	}, nil
}
