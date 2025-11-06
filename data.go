package ipfs

// Data -
type Data struct {
	Raw          []byte
	Node         string
	ResponseTime int64
}

// Provider -
type Provider struct {
	ID      string `validate:"required" yaml:"id"`
	Address string `validate:"required" yaml:"addr"`
}

// prefix
const (
	IpfsLinkPrefix = "ipfs://"
)
