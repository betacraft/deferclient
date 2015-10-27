package deferclient

// Command contains information about this client's command, that has to be executed
type Command struct {
	Id            int  `json:"id"`
	GenerateTrace bool `json"generateTrace"`
	Requested     bool `json:"requested"`
	Executed      bool `json:"executed"`
}

// NewCommand instantitates and returns a new command
// it is meant to be called once before the executing client's command
func NewCommand(id int, generatetrace bool, requested bool, executed bool) *Command {
	c := &Command{
		Id:            id,
		GenerateTrace: generatetrace,
		Requested:     requested,
		Executed:      executed,
	}

	return c
}
