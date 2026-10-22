module github.com/ghazichaieb/gola-bootlace

go 1.27.2

require (
	github.com/CalebQ42/squashfs v1.4.1
	github.com/klauspost/compress v1.20.3
	github.com/sonroyaalmerol/go-smb-server v0.2.0
	github.com/ulikunitz/xz v0.5.12
)

require (
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/rasky/go-lzo v0.0.0-20200203143853-96a758eda86e // indirect
	golang.org/x/crypto v0.54.0 // indirect
)

replace github.com/sonroyaalmerol/go-smb-server => ./third_party/go-smb-server

replace github.com/rasky/go-lzo => ./third_party/go-lzo-stub
