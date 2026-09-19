package ui

type State = state

const StateSearch = stateSearch

func (m *Model) SetState(s State)      { m.state = s }
func (m *Model) SetSize(w, h int)      { m.width, m.height = w, h }
func (m *Model) SwitchAPI(name string) { m.switchAPI(name) }
