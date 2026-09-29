PREFIX ?= $(HOME)/.local
APP_ID = eu.libormacak.Klient
VERSION ?= 0.1.0
LDFLAGS = -s -w -X github.com/Imbecile6197/klient/internal/ui.Version=$(VERSION)
RPMTOP = $(CURDIR)/build/rpm
PKG = klient-$(VERSION)

.PHONY: build run install uninstall test vet rpm release clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/klient ./cmd/klient

run: build
	./bin/klient

test:
	go test ./internal/...

vet:
	go vet ./...

install: build
	install -Dm755 bin/klient $(PREFIX)/bin/klient
	install -Dm644 data/$(APP_ID).desktop $(PREFIX)/share/applications/$(APP_ID).desktop
	install -Dm644 data/$(APP_ID).svg $(PREFIX)/share/icons/hicolor/scalable/apps/$(APP_ID).svg
	install -Dm644 data/$(APP_ID)-symbolic.svg $(PREFIX)/share/icons/hicolor/symbolic/apps/$(APP_ID)-symbolic.svg
	install -Dm644 data/$(APP_ID).metainfo.xml $(PREFIX)/share/metainfo/$(APP_ID).metainfo.xml
	-update-desktop-database $(PREFIX)/share/applications
	-gtk-update-icon-cache -f -t $(PREFIX)/share/icons/hicolor

uninstall:
	rm -f $(PREFIX)/bin/klient
	rm -f $(PREFIX)/share/applications/$(APP_ID).desktop
	rm -f $(PREFIX)/share/icons/hicolor/scalable/apps/$(APP_ID).svg
	rm -f $(PREFIX)/share/icons/hicolor/symbolic/apps/$(APP_ID)-symbolic.svg
	rm -f $(PREFIX)/share/metainfo/$(APP_ID).metainfo.xml

# Fedora package: build/rpm/RPMS/x86_64/klient-$(VERSION)-1.fcNN.x86_64.rpm
rpm: build
	rm -rf $(RPMTOP)
	mkdir -p $(RPMTOP)/SOURCES $(RPMTOP)/stage/$(PKG)
	cp bin/klient LICENSE README.md data/$(APP_ID).desktop data/$(APP_ID).svg \
	   data/$(APP_ID)-symbolic.svg data/$(APP_ID).metainfo.xml $(RPMTOP)/stage/$(PKG)/
	tar -C $(RPMTOP)/stage -czf $(RPMTOP)/SOURCES/$(PKG).tar.gz $(PKG)
	rpmbuild -bb packaging/klient.spec \
	   --define "_topdir $(RPMTOP)" --define "klient_version $(VERSION)"
	@echo
	@ls -1 $(RPMTOP)/RPMS/*/*.rpm

# GitHub release with the RPM and SHA256SUMS (needs gh and a tag vVERSION).
release:
	packaging/release.sh $(VERSION)

clean:
	rm -rf bin build
