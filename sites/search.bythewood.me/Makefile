.PHONY: run llm llm-stop llm-logs build clean

SUDO ?= sudo
LLM_IMAGE ?= overshard/aiagent:latest
LLM_MODEL ?= unsloth/Qwen3.5-4B-GGUF:Q4_K_M
LLM_NAME ?= search-llm
LLM_NET ?= search-net

# The model needs the GPU and this dev container was not started with --gpus,
# so llama-server runs in its own container on a shared network. LLAMA_ARG_OFFLINE
# is baked into the image for privacy and has to be turned off to resolve a
# model from Hugging Face the first time.
llm:
	-$(SUDO) docker network create $(LLM_NET) 2>/dev/null
	-$(SUDO) docker volume create search-models 2>/dev/null
	-$(SUDO) docker rm -f $(LLM_NAME) 2>/dev/null
	$(SUDO) docker run -d --name $(LLM_NAME) --gpus all \
		--restart unless-stopped \
		--network $(LLM_NET) \
		-v search-models:/models \
		-e LLAMA_ARG_OFFLINE=0 -e LLAMA_CACHE=/models \
		--entrypoint llama-server $(LLM_IMAGE) \
		-hf $(LLM_MODEL) \
		--host 0.0.0.0 --port 8091 \
		--n-gpu-layers 999 --flash-attn on --parallel 1 --jinja --no-mmproj \
		--ctx-size 32768 --cache-type-k q8_0 --cache-type-v q8_0 --ubatch-size 256
	@echo "waiting for the model to load..."
	@until curl -sf http://$(LLM_NAME):8091/health >/dev/null 2>&1; do sleep 2; done
	@echo "model ready on http://$(LLM_NAME):8091"

llm-stop:
	-$(SUDO) docker rm -f $(LLM_NAME)

llm-logs:
	$(SUDO) docker logs -f $(LLM_NAME)

run:
	LLM_URL=$${LLM_URL:-http://$(LLM_NAME):8091} go run .

build:
	go build -trimpath -ldflags="-s -w" -o ../../bin/search.bythewood.me .

clean:
	rm -rf build
