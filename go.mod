module clustta

go 1.25.0

require (
	github.com/DataDog/zstd v1.5.7
	github.com/alexedwards/scs/sqlite3store v0.0.0-20251002162104-209de6e426de
	github.com/alexedwards/scs/v2 v2.9.0
	github.com/eaxum/clustta-core v0.7.0
	github.com/getlantern/systray v1.2.2
	github.com/google/uuid v1.6.0
	github.com/jmoiron/sqlx v1.4.0
	github.com/jotfs/fastcdc-go v0.2.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/klauspost/compress v1.18.5
	github.com/mattn/go-sqlite3 v1.14.37
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646
	github.com/rs/cors v1.11.1
	github.com/zalando/go-keyring v0.2.4
	github.com/zeebo/xxh3 v1.1.0
	golang.org/x/crypto v0.49.0
	golang.org/x/text v0.35.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/mail.v2 v2.3.1
)

require (
	github.com/alessio/shellescape v1.4.2 // indirect
	github.com/danieljoos/wincred v1.2.1 // indirect
	github.com/getlantern/context v0.0.0-20190109183933-c447772a6520 // indirect
	github.com/getlantern/errors v0.0.0-20190325191628-abdb3e3e36f7 // indirect
	github.com/getlantern/golog v0.0.0-20190830074920-4ef2e798c2d7 // indirect
	github.com/getlantern/hex v0.0.0-20190417191902-c6586a6fe0b7 // indirect
	github.com/getlantern/hidden v0.0.0-20190325191715-f02dbb02be55 // indirect
	github.com/getlantern/ops v0.0.0-20190325191751-d70cb0d6f85f // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
)

replace github.com/eaxum/clustta-core => ../clustta-core
