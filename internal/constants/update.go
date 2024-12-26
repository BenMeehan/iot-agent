package constants

type UpdateState string

const (
	StateDownloading UpdateState = "downloading"
	StateVerifying   UpdateState = "verifying"
	StateInstalling  UpdateState = "installing"
	StateSuccess     UpdateState = "success"
	StateFailure     UpdateState = "failure"
)
