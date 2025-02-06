TARGETS := $(shell ls scripts)
MACHINE := rancher
# Define the target platforms that can be used across the ecosystem.
# Note that what would actually be used for a given project will be
# defined in TARGET_PLATFORMS, and must be a subset of the below:
DEFAULT_PLATFORMS := linux/amd64,linux/arm64,darwin/arm64,darwin/amd64

.dapper:
	@echo Downloading dapper
	@curl -sL https://releases.rancher.com/dapper/latest/dapper-`uname -s`-`uname -m` > .dapper.tmp
	@@chmod +x .dapper.tmp
	@./.dapper.tmp -v
	@mv .dapper.tmp .dapper

$(TARGETS): .dapper
	./.dapper -f Dockerfile --target builder_base $@

.PHONY: buildx-machine
buildx-machine:
	docker buildx ls | grep $(MACHINE) >/dev/null || \
		docker buildx create --name=$(MACHINE) --platform=$(DEFAULT_PLATFORMS)

# variables needed from GHA caller:
# - REPO: image repo, include $registry/$repo_path
# - TAG: image tag
# - TARGET_PLATFORMS: optional, to be passed for buildx's --platform option
# - IID_FILE_FLAG: optional, options to generate image ID file
.PHONY: workflow-image-build-push workflow-image-build-push-secure workflow-manifest-image
workflow-image-build-push: buildx-machine
	MACHINE=$(MACHINE) OUTPUT_ARGS='--push' bash scripts/package
workflow-image-build-push-secure: buildx-machine
	MACHINE=$(MACHINE) OUTPUT_ARGS='--push' IS_SECURE=true bash scripts/package
workflow-manifest-image:
	docker pull --platform linux/amd64 ${REPO}/longhorn-instance-manager:${TAG}-amd64
	docker pull --platform linux/arm64 ${REPO}/longhorn-instance-manager:${TAG}-arm64
	docker buildx imagetools create -t ${REPO}/longhorn-instance-manager:${TAG} \
	  ${REPO}/longhorn-instance-manager:${TAG}-amd64 \
	  ${REPO}/longhorn-instance-manager:${TAG}-arm64

trash: .dapper
	./.dapper -m bind trash

trash-keep: .dapper
	./.dapper -m bind trash -k

deps: trash

.DEFAULT_GOAL := ci

.PHONY: $(TARGETS)
