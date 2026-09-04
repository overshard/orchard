.PHONY: run build clean

# The model is a service in docker-compose.yml, not something started here, so
# `make deploy SITE=search.bythewood.me` from the repo root brings up both.
#
# For running the site outside Docker against the deployed model:
#
#   sudo docker run --rm -d --name search-llm-dev --gpus all \
#     --network orchard-search-model -v orchard-search-models:/models \
#     -e LLAMA_ARG_OFFLINE=0 -e LLAMA_CACHE=/models \
#     --entrypoint llama-server overshard/aiagent:latest -hf unsloth/Qwen3.5-4B-GGUF:Q4_K_M \
#     --host 0.0.0.0 --port 8091 --n-gpu-layers 999 --flash-attn on --parallel 1 \
#     --jinja --no-mmproj --ctx-size 32768

# SEARCH_DEV_NOAUTH skips the auth check, which only works in a build without
# -tags embed. See devOpen in main.go.
run:
	SEARCH_DEV_NOAUTH=1 LLM_URL=$${LLM_URL:-http://orchard-search-llm:8091} go run .

build:
	go build -trimpath -ldflags="-s -w" -o ../../bin/search.bythewood.me .

clean:
	rm -rf build
