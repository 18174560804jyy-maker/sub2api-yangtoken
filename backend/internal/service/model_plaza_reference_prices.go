package service

// plazaOfficialReferenceFallback is display-only: never use these reference
// prices in BillingService or the shared pricing catalog. Verified 2026-11-09.
// Catalog entries take precedence when the upstream catalog gains coverage.
func plazaOfficialReferenceFallback(model string) *PlazaOfficialPricing {
	perMillion := func(value float64) *float64 {
		price := value / 1_000_000
		return &price
	}
	var input, output, read float64
	var write, write1h *float64
	var source, note string
	switch model {
	case "qwen3.8-max":
		input, output, read = 2, 6, 0.17
		write = perMillion(2.5)
		source = "https://www.alibabacloud.com/help/en/model-studio/qwen3-8-max"
	case "qwen3.7-max":
		input, output, read = 2.5, 7.5, 0.25
		write = perMillion(3.125)
		source = "https://www.alibabacloud.com/help/en/model-studio/qwen3-7-max"
	case "qwen3.8-flash":
		input, output, read = 0.15, 0.47, 0.016
		write = perMillion(0.2)
		source = "https://www.alibabacloud.com/help/en/model-studio/qwen3-8-flash"
	case "mimo-v2.6-pro", "mimo-v2.6-flash":
		input, output, read = 0.435, 0.87, 0.0036
		if model == "mimo-v2.6-flash" {
			input, output, read = 0.14, 0.28, 0.0028
		}
		write = perMillion(0)
		source = "https://mimo.mi.com/docs/en-US/price/pay-as-you-go"
		note = "Overseas real-time API, USD/1M tokens; cache writes temporarily free. Verified 2026-11-09."
	case "kimi-k3.1-pro":
		input, output, read = 2, 10, 0.2
		write, write1h = perMillion(2), perMillion(4)
		source = "https://platform.kimi.ai/docs/pricing/chat"
		note = "International standard API, USD/1M tokens; cache writes: 5m / 1h. Verified 2026-11-09."
	case "hy4-preview", "hy3":
		input, output, read = 0.834, 2.501, 0.042
		if model == "hy3" {
			input, output, read = 0.132, 0.528, 0.033
		}
		source = "https://intl.cloud.tencent.com/zh/document/product/1300/78937"
		note = "Tencent Cloud Singapore list price, USD/1M tokens; excludes promotions. Verified 2026-11-09."
	default:
		return nil
	}
	if note == "" {
		note = "Alibaba Cloud Singapore (International), USD/1M tokens; explicit cache creation/read, excludes promotions. Verified 2026-11-09."
	}
	return &PlazaOfficialPricing{
		InputPrice: perMillion(input), OutputPrice: perMillion(output),
		CacheWritePrice: write, CacheWrite1hPrice: write1h, CacheReadPrice: perMillion(read),
		SourceURL: source, SourceNote: note,
	}
}
