package constants

type UpdateState string

const (
	UpdateStateIdle        UpdateState = "idle"
	UpdateStateDownloading UpdateState = "downloading"
	UpdateStateVerifying   UpdateState = "verifying"
	UpdateStateInstalling  UpdateState = "installing"
	UpdateStateSuccess     UpdateState = "success"
	UpdateStateFailure     UpdateState = "failure"
)
