package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// runInit seeds the account and prints its first recovery codes, needing no
// ntfy, and leaves an existing account alone. It runs by docker exec and never on
// boot, since container stdout ships to logging and codes must never land there.
func runInit(db *sql.DB) error {
	created, err := createUser(db, seedUsername)
	if err != nil {
		return err
	}
	if !created {
		user, err := loadUser(db)
		if err != nil {
			return err
		}
		remaining, err := countRecoveryCodes(db)
		if err != nil {
			return err
		}
		fmt.Printf("already initialized as %q, with %d recovery codes left, and nothing was changed.\n",
			user.Username, remaining)
		fmt.Println("to replace the codes, sign in and use the security page.")
		return nil
	}

	codes, err := regenerateRecoveryCodes(db)
	if err != nil {
		return err
	}

	fmt.Printf("\nthe account is %q, and these are its recovery codes:\n\n", seedUsername)
	for _, code := range codes {
		fmt.Printf("  %s\n", code)
	}
	fmt.Printf("\nthese are the only copies, so put them in Bitwarden now. each one\n")
	fmt.Printf("works once. sign in with one at %s/recovery, then\n", baseURL)
	fmt.Printf("subscribe the phone to the %q topic and regenerate them from the\n", ntfyTopic)
	fmt.Printf("security page, so the set you keep was never on a terminal.\n")
	return nil
}

// runCheck reports what exists without changing anything, for `make doctor`. A
// second -init would create the account on a machine that has none.
func runCheck(db *sql.DB) error {
	user, err := loadUser(db)
	if errors.Is(err, errNoUser) {
		fmt.Println("not initialized")
		return nil
	}
	if err != nil {
		return err
	}
	remaining, err := countRecoveryCodes(db)
	if err != nil {
		return err
	}
	fmt.Printf("initialized as %s, %d recovery codes left\n", user.Username, remaining)
	return nil
}

// runRecovery replaces the recovery codes for when none are left and ntfy or the
// tunnel is down. It needs the host's Docker socket, so every credential here can
// be replaced from the machine and none of them has to be kept anywhere.
func runRecovery(db *sql.DB) error {
	if _, err := loadUser(db); errors.Is(err, errNoUser) {
		return fmt.Errorf("not initialized yet, run -init instead")
	} else if err != nil {
		return err
	}

	codes, err := regenerateRecoveryCodes(db)
	if err != nil {
		return err
	}

	fmt.Printf("\nevery previous recovery code stopped working just now. the new set:\n\n")
	for _, code := range codes {
		fmt.Printf("  %s\n", code)
	}
	fmt.Printf("\neach one works once, at %s/recovery.\n", baseURL)
	return nil
}
