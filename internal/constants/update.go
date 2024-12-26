package constants

type UpdateState string

const (
	StateEmpty       UpdateState = ""
	StateDownloading UpdateState = "downloading"
	StateVerifying   UpdateState = "verifying"
	StateInstalling  UpdateState = "installing"
	StateSuccess     UpdateState = "success"
	StateFailure     UpdateState = "failure"
)
