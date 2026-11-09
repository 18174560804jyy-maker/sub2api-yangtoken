package service

// plazaOfficialReference uses the CNY list prices recorded on HeroHao and
// supplied by the operator on 2026-11-09. These display-only references must
// never enter BillingService or the shared pricing catalog.
func plazaOfficialReference(model string) *PlazaOfficialPricing {
	perMillion := func(value float64) *float64 {
		price := value / 1_000_000
		return &price
	}
	var input, output, read, write float64
	switch model {
	case "deepseek-v4-pro-0813":
		input, output, read, write = 4.5, 13.5, 0.15, 0
	case "deepseek-v4.1-flash":
		input, output, read, write = 1, 4, 0.02, 0
	case "glm-5.3":
		input, output, read, write = 8, 28, 2, 0
	case "glm-5.3-flash":
		input, output, read, write = 1, 3.35, 0.23, 0
	case "glm-5.3-flashx":
		input, output, read, write = 2, 7, 0.57, 0
	case "hy3":
		input, output, read, write = 1, 4, 0.25, 0
	case "hy4-preview":
		input, output, read, write = 6, 18, 0.3, 0
	case "kimi-k3":
		input, output, read, write = 20, 100, 2, 0
	case "kimi-k3.1-pro":
		input, output, read, write = 13, 68, 1.3, 0
	case "mimo-v2.6-flash":
		input, output, read, write = 2, 4, 0.06, 0
	case "mimo-v2.6-pro":
		input, output, read, write = 4.35, 8.7, 0.036, 0
	case "minimax-m3":
		input, output, read, write = 2.1, 8.4, 0.42, 0
	case "minimax-m3.1-flash":
		input, output, read, write = 1, 4, 0.12, 0
	case "qwen3.7-max":
		input, output, read, write = 12, 36, 2.4, 15
	case "qwen3.8-flash":
		input, output, read, write = 0.8, 2.7, 0.16, 1
	case "qwen3.8-max":
		input, output, read, write = 12, 36, 2.4, 15
	default:
		return nil
	}
	return &PlazaOfficialPricing{
		Currency:   "CNY",
		InputPrice: perMillion(input), OutputPrice: perMillion(output),
		CacheReadPrice: perMillion(read), CacheWritePrice: perMillion(write),
	}
}
