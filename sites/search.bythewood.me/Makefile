.PHONY: run build clean

# Two services, both in docker-compose.yml, so `make deploy SITE=search.bythewood.me`
# from the repo root builds and brings up the site and its model together.
#
# The model image is llm/, built here rather than borrowed: llama.cpp compiled
# for this card behind llama-swap, which starts the model on demand and unloads
# it after the ttl in llm/config.yaml. The first build compiles CUDA kernels and
# takes a while; after that it is cached.
#
# To run the site outside Docker against the deployed model, join its network:
#
#   sudo docker network connect orchard-search-model bythewood-webdev
#   make run

# SEARCH_DEV_NOAUTH skips the auth check, which only works in a build without
# -tags embed. See devOpen in main.go.
run:
	SEARCH_DEV_NOAUTH=1 LLM_URL=$${LLM_URL:-http://orchard-search-llm:8091} go run .

build:
	go build -trimpath -ldflags="-s -w" -o ../../bin/search.bythewood.me .

clean:
	rm -rf build
