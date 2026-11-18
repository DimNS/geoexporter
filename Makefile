.PHONY: test release

test:
	@go test ./...

release:
	@$(HOME)/.local/bin/release.sh $(if $(DRY_RUN),--dry-run)
