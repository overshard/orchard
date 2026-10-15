.PHONY: run build clean

# There's no Vite here. templates/ and static/ are written by hand and a dev
# build reads both off disk on every request, so an edit shows on a reload with
# no restart and there's no manifest to wait for.
#
# The model is wherever LLM_URL and LLM_KEY in the environment say, and the
# conversations go in data/, which clean leaves alone.
run:
	go run .

# -tags embed compiles templates/ and static/ into the binary.
build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/chat.bythewood.me .

# Nothing is generated inside this directory, so the binary is all there is.
clean:
	rm -f ../../bin/chat.bythewood.me
