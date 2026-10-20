package model

// Asset is an image used by a storefront: the shop avatar and cover, the
// decoration images of the theme and the cover of a product.
//
// The bytes are copied into a module owned table instead of pointing at a file
// of the seller's drive: a storefront is public, while drive files are private.
// Copying on pick also means a later rename, move or delete in the drive never
// breaks a published shop, and a visitor can never use an asset URL to read a
// file the seller did not explicitly publish.
type Asset struct {
	// ID is the public identifier of the asset: it is random and unguessable
	// because the serving endpoint is anonymous.
	ID string
	// UID is the owner of the asset. Only the owner can list or reuse it.
	UID       int
	Name      string
	Mime      string
	Size      int64
	Data      []byte
	CreatedAt int64
}
