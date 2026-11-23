.PHONY: run build clean

# There's no Vite here. templates/ are written by hand and the look comes from
# suite/. Point PASS_VAULT at a copy of Vaultwarden's db.sqlite3 for a dev run.
run:
	PASS_VAULT=$${PASS_VAULT:-data/db.sqlite3} go run .

build:
	go build -trimpath -ldflags="-s -w" -o ../../bin/pass.bythewood.me .

clean:
	rm -f ../../bin/pass.bythewood.me
