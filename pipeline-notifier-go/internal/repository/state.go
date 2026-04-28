package repository

type State struct {
	PipelineID  string
	Status      string
	Timestamp   string
	LastEventID string
}

var db = make(map[string]State)

func GetState(id string) *State {
	if val, ok := db[id]; ok {
		return &val
	}
	return nil
}

func SaveState(s State) {
	db[s.PipelineID] = s
}
