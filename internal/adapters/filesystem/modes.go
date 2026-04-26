package filesystem

import "os"

const (
	privateDirMode  os.FileMode = 0o750
	privateFileMode os.FileMode = 0o600
	metadataMode    os.FileMode = 0o644
)
