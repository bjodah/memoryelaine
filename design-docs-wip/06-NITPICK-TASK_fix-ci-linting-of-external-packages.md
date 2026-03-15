Currently I see this in my CI log:
```
==> Building packages
+ bash -lc 'curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.11.1'
golangci/golangci-lint info checking GitHub for tag 'v2.11.1'
golangci/golangci-lint info found version: 2.11.1 for v2.11.1/linux/amd64
golangci/golangci-lint info installed /root/go/bin/golangci-lint
+ bash -lc "./scripts/run-lint-checks.sh"
==> Checking formatting with gofmt
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue20789.go:9:51: expected '(', found u
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue23385.go:10:5: expected boolean expression, found assignment (missing parentheses around composite literal?)
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue23385.go:15:5: expected boolean expression, found assignment (missing parentheses around composite literal?)
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue23434.go:11:38: expected type, found newline
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue23434.go:13:49: expected type, found newline
```
We clearly should be excluding linting of external packages.

<TODO> write a plan here for how to do </TODO>
