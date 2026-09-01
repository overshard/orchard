package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// runInit seeds the account and prints the first set of recovery codes, which
// are what the first login uses. Nothing here needs ntfy, so a fresh machine can
// get in before the phone is enrolled and mint everything else from inside.
//
// It is idempotent the same way `ntfy up` is: an account that already exists
// keeps what it has and nothing is printed, because handing back a set of codes
// that were never applied is worse than printing nothing.
//
// This runs as `docker exec orchard-auth /app -init`, which works against a
// scratch image because exec wants a binary path rather than a shell. It must
// never generate on first boot and print to stdout, since container stdout ships
// to logging.bythewood.me and that would write recovery codes into a store with
// a long retention on it.
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
	fmt.Printf("\nthese are the only copies, so put them in 1Password now. each one\n")
	fmt.Printf("works once. sign in with one at %s/recovery, then\n", baseURL)
	fmt.Printf("subscribe the phone to the %q topic and regenerate them from the\n", ntfyTopic)
	fmt.Printf("security page, so the set you keep was never on a terminal.\n")
	return nil
}

// runCheck reports what exists without changing anything, which is what
// `make doctor` calls. It has to be its own flag rather than a second run of
// -init, since that would create the account on a machine that has none.
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
