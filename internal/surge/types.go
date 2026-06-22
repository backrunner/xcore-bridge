package surge

import "github.com/backrunner/xcore-bridge/internal/vless"

type InstallOptions struct {
	Nodes     []vless.Node
	ExecPath  string
	BasePort  int
	WriteFile bool

	portAvailable func(string, int) bool
}

type RemoveOptions struct {
	Names     []string
	WriteFile bool
}

type InstallResult struct {
	Profile     string
	PolicyNames []string
	LocalPorts  []int
	BackupPath  string
}

type RemoveResult struct {
	Profile      string
	RemovedNames []string
	BackupPath   string
}

type ProfileCandidate struct {
	Path   string
	Source string
}
