# WeatherScr — build entry points
#
# The single check is `make gate`. It is the project's external judge: build, vet,
# tests, Windows cross-compile, and a privacy scan of the tree.

GO      ?= go
LDFLAGS := -s -w
WINENV  := GOOS=windows GOARCH=amd64 CGO_ENABLED=0
LIBS    := ./ui ./config ./weather ./effect ./scrargs ./geo
RSRC    ?= rsrc

.PHONY: all scr payload setup fxshot wfetch winlist gate test vet fmt icon clean

all: scr setup

# The screensaver. Windows-only: gioui needs a window system on Linux.
scr:
	$(WINENV) $(GO) build -ldflags="$(LDFLAGS)" -o WeatherScr.scr ./cmd/scr

# Stage the screensaver as the installer payload. The file tracked in git is a
# placeholder so a fresh clone compiles; this target replaces it with the real one.
# Do not commit the result.
payload: scr
	cp -f WeatherScr.scr cmd/setup/payload/WeatherScr.scr
	@echo "payload staged. Do not commit cmd/setup/payload/WeatherScr.scr."

setup: payload
	$(WINENV) $(GO) build -ldflags="$(LDFLAGS)" -o WeatherScr_setup.exe ./cmd/setup

# Headless renderer: iterates the visuals with no display, on Linux.
fxshot:
	GOFLAGS="-tags=novulkan" $(GO) build -o fxshot ./cmd/fxshot

wfetch:
	GOFLAGS="-tags=novulkan" $(GO) build -o wfetch ./cmd/wfetch

winlist:
	$(WINENV) $(GO) build -ldflags="$(LDFLAGS)" -o winlist.exe ./cmd/winlist

test:
	GOFLAGS="-tags=novulkan" $(GO) test -count=1 $(LIBS)

vet:
	GOFLAGS="-tags=novulkan" $(GO) vet $(LIBS)
	$(WINENV) GOFLAGS="-mod=mod" $(GO) vet ./cmd/...

fmt:
	$(GO) fmt $(LIBS) ./cmd/scr ./cmd/setup ./cmd/fxshot ./cmd/wfetch ./cmd/winlist

# Regenerate the Windows icon resource from icons/WeatherScr.ico.
# Needs rsrc: go install github.com/akavel/rsrc@latest
icon:
	$(RSRC) -ico icons/WeatherScr.ico -o cmd/scr/rsrc_windows_amd64.syso
	$(RSRC) -ico icons/WeatherScr.ico -o cmd/setup/rsrc_windows_amd64.syso
	@echo "regenerated rsrc_windows_amd64.syso for cmd/scr and cmd/setup"

# The single external check.
gate:
	GOFLAGS="-mod=mod -tags=novulkan" $(GO) build $(LIBS)
	GOFLAGS="-mod=mod -tags=novulkan" $(GO) vet $(LIBS)
	GOFLAGS="-mod=mod -tags=novulkan" $(GO) test -count=1 $(LIBS)
	$(WINENV) GOFLAGS="-mod=mod" $(GO) vet ./cmd/...
	$(WINENV) GOFLAGS="-mod=mod" $(GO) build -ldflags="$(LDFLAGS)" -o /tmp/WeatherScr_gate.scr ./cmd/scr
	! grep -rn --include='*.go' --include='*.json' --include='*.md' --include='*.txt' \
		-e '验收' -e '拍板' -e '点名' -e '上一轮' -e '本轮' -e '选错' -e '批次' \
		-e '实机' -e '真机' -e '用户' -e '「' -e '」' -e '“' -e '”' -e '？' \
		-e 'Ziliujing' -e '自流井' -e 'Zigong' -e '29\.2772' -e '104\.7044' \
		-e '192\.168' -e 'DESKTOP-HOPJST3' -e '/home/loong' -e 'Administrator' \
		-e 'lane[0-9]' -e 'spec/[A-Za-z0-9_-]*\.md' -e '§' $(CURDIR)
	@echo "gate PASS"

clean:
	rm -f WeatherScr.scr WeatherScr_setup.exe fxshot wfetch winlist.exe
	rm -f $(LIBS:%=%/*.test)
