package adapter

// Embed is a rich message in the shape melodix actually writes them.
//
// Discord's own struct has around fifteen fields and the commands use six, so
// this is the six. It exists so a command can describe what it wants to say
// without naming the library that will put it on the wire — the rendering
// lives with the backend that sends it.
//
// Colour is deliberately a plain int rather than a named type: zero means
// "whatever the responder's default is", which is what almost every caller
// wants and none of them should have to say.
type Embed struct {
	Title       string
	Description string
	Color       int
	Footer      string
	ImageURL    string
	Fields      []EmbedField
}

// EmbedField is one name/value row. Inline packs rows side by side.
type EmbedField struct {
	Name   string
	Value  string
	Inline bool
}
