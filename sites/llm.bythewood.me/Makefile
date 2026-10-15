.PHONY: run build clean

# There's no Vite here. templates/ and static/ are written by hand and a dev
# build reads both off disk on every request, so an edit shows on a reload with
# no restart and there's no manifest to wait for.
#
# The model server and the gpu probe are names on compose's private network, so
# a dev run serves its pages and can't reach either until LLM_UPSTREAM and
# LLM_GPU point somewhere real. The call log goes in data/, which clean leaves
# alone.
run:
	go run .

# -tags embed compiles templates/ and static/ into the binary.
build:
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/llm.bythewood.me .

# Nothing is generated inside this directory, so the binary is all there is.
clean:
	rm -f ../../bin/llm.bythewood.me
