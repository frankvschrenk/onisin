package helper

type CallInvite struct {
	From       string
	Me         string
	RoomName   string
	Token      string
	LiveKitURL string
	Incoming   bool
}

func BuildCallInvite(from, me, token, livekitURL, room string, incoming bool) *CallInvite {
	return &CallInvite{
		From:       from,
		Me:         me,
		RoomName:   room,
		Token:      token,
		LiveKitURL: livekitURL,
		Incoming:   incoming,
	}
}

var CallInviteFn func(invite *CallInvite)
