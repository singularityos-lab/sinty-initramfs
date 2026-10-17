# Contributing to sinty-initramfs

Thanks for your interest in contributing!

## Quick Start

```bash
git clone https://github.com/singularityos-lab/sinty-initramfs
cd sinty-initramfs
go build ./...
go test ./...
```

## Guidelines

- Keep changes additive and tested. sinty-initramfs decides which root a
  machine boots; prefer small, verifiable commits over large rewrites.
- Boot logic that is not particular to SintyOS belongs in the Atom Loops
  `boot` package, not here.
- Match the surrounding style. Comments explain WHY, not WHAT.
- By submitting a contribution you agree to the [CLA](CLA.md).

## License

GPL-3.0-only.
