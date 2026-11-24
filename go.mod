module github.com/ieyoukan/daigaku-hack

go 1.25.0

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	github.com/google/uuid v1.6.0
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	golang.org/x/net v0.55.0
)

require golang.org/x/text v0.37.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.46.0 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.52.0
)

replace github.com/ledongthuc/pdf => ./third_party/ledongthuc-pdf
