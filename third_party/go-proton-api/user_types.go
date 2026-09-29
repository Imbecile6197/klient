package proton

type User struct {
	ID          string
	Name        string
	DisplayName string
	Email       string
	Keys        Keys

	UsedSpace uint64
	MaxSpace  uint64
	MaxUpload uint64

	Credit   int
	Currency string

	ProductUsedSpace ProductUsedSpace

	// klient patch: split storage (mail+calendar vs. Drive); nil on plans
	// with one shared quota.
	UsedBaseSpace  *uint64
	MaxBaseSpace   *uint64
	UsedDriveSpace *uint64
	MaxDriveSpace  *uint64
}

type DeleteUserReq struct {
	Reason   string
	Feedback string
	Email    string
}

type ProductUsedSpace struct {
	Calendar uint64
	Contact  uint64
	Drive    uint64
	Mail     uint64
	Pass     uint64
}
