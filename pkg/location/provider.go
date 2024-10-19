package location

// Provider interface defines the methods for location providers
type Provider interface {
	GetLocation() (Location, error)
}
