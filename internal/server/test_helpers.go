package server

// NewTestClient creates a Client with the given userID for testing purposes.
// The conn field is nil, which is fine for handler tests that only read UserID.
// Note: This function is intended for use in tests only.
func NewTestClient(userID string) *Client {
	return &Client{userID: userID}
}

// NewTestClientWithConnID creates a Client with both userID and connID set for testing purposes.
// The conn field is nil, which is fine for handler tests that only read UserID/ConnID.
// Note: This function is intended for use in tests only.
func NewTestClientWithConnID(userID, connID string) *Client {
	return &Client{userID: userID, connID: connID}
}
