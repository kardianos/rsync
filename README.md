# rsync

A pure Go implementation of the [rsync algorithm](https://rsync.samba.org/tech_report/), similar to librsync.
This library provides the core primitives for efficiently synchronizing data over a network or locally by transferring only the differences.

## Features

- **Pure Go**: No CGO dependencies.
- **Rdiff CLI**: Includes a clone of the `rdiff` utility.
- **Components**:
  - `Signature`: Generate a signature of a file (rolling checksums).
  - `Delta`: Calculate the difference between a signature and a new file.
  - `Patch`: Apply a delta to a basis file to reconstruct the new file.

## Usage

### Library

```go
import "github.com/kardianos/rsync"

// Create Signature
rs := &rsync.RSync{}
rs.CreateSignature(readers, signatureWriter)

// Create Delta
rs.CreateDelta(reader, signature, operationWriter, nil)

// Apply Delta
rs.ApplyDelta(writer, basisReader, operationChannel, nil)
```

### CLI (rdiff)

The `rdiff` subdirectory contains a command-line tool compatible with the standard `rdiff`.

```bash
# Generate signature
rdiff signature basis.file basis.sig

# Generate delta
rdiff delta basis.sig new.file delta.file

# Apply patch
rdiff patch basis.file delta.file reconstructed.file
```
