.PHONY: all build test lint coverage security check clean install-local install-local-force dev fmt tidy
.PHONY: test-unit ci help tools watch install-service uninstall-service

# Build configuration
BINARY_DIR := bin
BINARY_NAME := sessions
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

# Default target
all: check build

# =============================================================================
# Build targets
# =============================================================================

build:
	@mkdir -p $(BINARY_DIR)
	go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/sessions
	@echo "$(GREEN)Built: $(BINARY_DIR)/$(BINARY_NAME)$(NC)"

# =============================================================================
# Test targets
# =============================================================================

test-unit:
	@echo "$(GREEN)Running unit tests...$(NC)"
	go test -v -race ./...

test: test-unit

# =============================================================================
# Code quality targets
# =============================================================================

fmt:
	@echo "$(GREEN)Formatting code...$(NC)"
	go fmt ./...
	@which goimports > /dev/null 2>&1 && goimports -w . || echo "$(YELLOW)goimports not installed, skipping$(NC)"

tidy:
	go mod tidy

vet:
	@echo "$(GREEN)Running go vet...$(NC)"
	go vet ./...

lint:
	@echo "$(GREEN)Running golangci-lint...$(NC)"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "$(YELLOW)golangci-lint not installed, running go vet instead$(NC)" && go vet ./...

security:
	@echo "$(GREEN)Running security checks...$(NC)"
	@which gosec > /dev/null 2>&1 && gosec -quiet ./... || echo "$(YELLOW)gosec not installed, skipping$(NC)"

coverage:
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	go test -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "$(GREEN)Coverage summary:$(NC)"
	@go tool cover -func=coverage.out | grep -E "^total:|internal/"
	@echo ""
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Full report: coverage.html$(NC)"

# =============================================================================
# CI / Golden Pipeline
# =============================================================================

ci: fmt tidy vet lint test-unit coverage build
	@echo ""
	@echo "$(GREEN)========================================$(NC)"
	@echo "$(GREEN)  CI Pipeline completed successfully!  $(NC)"
	@echo "$(GREEN)========================================$(NC)"

check: fmt tidy vet test-unit build
	@echo "$(GREEN)Quick check passed!$(NC)"

# =============================================================================
# Utility targets
# =============================================================================

clean:
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html

install-local: build
	@mkdir -p ~/.local/bin
	@if [ -x ~/.local/bin/$(BINARY_NAME) ]; then \
		echo "$(YELLOW)Current:$(NC) $$(~/.local/bin/$(BINARY_NAME) version 2>/dev/null || echo 'unknown')"; \
		echo "$(GREEN)New:$(NC)     $(BINARY_NAME) version $(VERSION) (built $(BUILD_TIME))"; \
		echo ""; \
		read -p "Replace existing installation? [y/N] " confirm; \
		if [ "$$confirm" != "y" ] && [ "$$confirm" != "Y" ]; then \
			echo "$(YELLOW)Cancelled$(NC)"; \
			exit 1; \
		fi; \
	else \
		echo "$(GREEN)Installing:$(NC) $(BINARY_NAME) version $(VERSION) (built $(BUILD_TIME))"; \
	fi
	cp $(BINARY_DIR)/$(BINARY_NAME) ~/.local/bin/
	@echo "$(GREEN)Installed to ~/.local/bin/$(BINARY_NAME)$(NC)"

install-local-force: build
	@mkdir -p ~/.local/bin
	cp $(BINARY_DIR)/$(BINARY_NAME) ~/.local/bin/
	@echo "$(GREEN)Installed to ~/.local/bin/$(BINARY_NAME)$(NC)"

dev: build
	./$(BINARY_DIR)/$(BINARY_NAME)

watch:
	find . -name '*.go' | entr -c make build

# =============================================================================
# Service targets
# =============================================================================

SERVICE_FILE := sessions-watcher.service
SERVICE_DIR := $(HOME)/.config/systemd/user

install-service: install-local-force
	@mkdir -p $(SERVICE_DIR)
	@echo '[Unit]' > $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'Description=Session index watcher for Claude Code' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo '' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo '[Service]' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'Type=simple' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'ExecStart=%h/.local/bin/$(BINARY_NAME) watch' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'Restart=on-failure' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'RestartSec=10' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'Environment=HOME=%h' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo '' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo '[Install]' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	@echo 'WantedBy=default.target' >> $(SERVICE_DIR)/$(SERVICE_FILE)
	systemctl --user daemon-reload
	systemctl --user enable $(SERVICE_FILE)
	systemctl --user restart $(SERVICE_FILE)
	@echo ""
	@echo "$(GREEN)Service installed and started.$(NC)"
	@echo "  Status:  systemctl --user status $(SERVICE_FILE)"
	@echo "  Logs:    journalctl --user -u $(SERVICE_FILE) -f"
	@echo "  Stop:    systemctl --user stop $(SERVICE_FILE)"
	@echo "  Disable: systemctl --user disable $(SERVICE_FILE)"

uninstall-service:
	-systemctl --user stop $(SERVICE_FILE) 2>/dev/null
	-systemctl --user disable $(SERVICE_FILE) 2>/dev/null
	rm -f $(SERVICE_DIR)/$(SERVICE_FILE)
	systemctl --user daemon-reload
	@echo "$(GREEN)Service removed.$(NC)"

tools:
	@echo "$(GREEN)Installing development tools...$(NC)"
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "$(GREEN)Tools installed!$(NC)"

help:
	@echo "Available targets:"
	@echo ""
	@echo "  $(GREEN)Build:$(NC)"
	@echo "    build          - Build sessions binary"
	@echo "    install-local  - Build and install to ~/.local/bin"
	@echo "    install-local-force - Install without confirmation"
	@echo ""
	@echo "  $(GREEN)Test:$(NC)"
	@echo "    test           - Run unit tests"
	@echo "    coverage       - Generate coverage report"
	@echo ""
	@echo "  $(GREEN)Quality:$(NC)"
	@echo "    fmt            - Format code"
	@echo "    vet            - Run go vet"
	@echo "    lint           - Run golangci-lint"
	@echo "    security       - Run security checks"
	@echo "    check          - Quick check (fmt, vet, test, build)"
	@echo ""
	@echo "  $(GREEN)CI Pipeline:$(NC)"
	@echo "    ci             - Full CI pipeline"
	@echo ""
	@echo "  $(GREEN)Service:$(NC)"
	@echo "    install-service   - Build, install, and start systemd watcher service"
	@echo "    uninstall-service - Stop and remove systemd service"
	@echo ""
	@echo "  $(GREEN)Utility:$(NC)"
	@echo "    tools          - Install development tools"
	@echo "    clean          - Remove build artifacts"
	@echo "    dev            - Build and run"
	@echo "    watch          - Rebuild on changes (requires entr)"
	@echo "    help           - Show this help"
