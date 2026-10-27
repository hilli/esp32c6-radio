package esp32c6radio

// EntropySourceState reports configured digital prerequisites, NOT proof of
// analogue entropy or cryptographic quality. Statistical tests cannot certify it.
type EntropySourceState struct {
	PHYActive      bool   `json:"phy_active"`
	FrontEndClocks bool   `json:"front_end_clocks"`
	RNGClock       bool   `json:"rng_clock"`
	CPUMHz         uint32 `json:"cpu_mhz"`
	APBMHz         uint32 `json:"apb_mhz"`
}
