package trip

//go:generate go tool go-enum --marshal --nocase --output-suffix=.enum

// ENUM(NONE="", ACTIVE="active", COMPLETED="completed")
type Status string
