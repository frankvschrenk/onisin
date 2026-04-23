package helper

var Stage struct {
	CurrentContext  string
	CurrentData     map[string]any
	CurrentFormData map[string]any
	LastQuery       string
}

var ActiveEntityWindow EntityWindow

type EntityWindow interface {
	GetContextName() string
	GetMergedData() map[string]any
	Reload()
}

var RenderFn func(screenName string)

var ClearScreenFn func()

var DashboardRefreshFn func()

func RenderScreen(screenName string, data map[string]any) {
	Stage.CurrentContext = screenName
	if data != nil {
		Stage.CurrentData = data
	}
	if RenderFn != nil {
		RenderFn(screenName)
	}
}

func RenderScreenWithForm(screenName string, data, formData map[string]any) {
	Stage.CurrentContext = screenName
	if data != nil {
		Stage.CurrentData = data
	}
	if formData != nil {
		Stage.CurrentFormData = formData
	}
	if RenderFn != nil {
		RenderFn(screenName)
	}
}

func ClearScreen() {
	Stage.CurrentContext = ""
	Stage.CurrentData = nil
	Stage.CurrentFormData = nil
	if ClearScreenFn != nil {
		ClearScreenFn()
	}
}

var BroadcastFn func(msg string)
