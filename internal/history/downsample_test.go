package history

import (
	"testing"
)

func TestDownsample(t *testing.T) {
	// 构造 1000 个采样点
	samples := make([]Sample, 1000)
	for i := 0; i < 1000; i++ {
		samples[i] = Sample{
			TS:    int64(i),
			CPU:   float32(i % 100),
			Mem:   50.0,
			NetRx: float64(i * 10),
		}
	}

	// 1. 当请求点数大于样本数时，不发生降采样
	same := Downsample(samples, 2000)
	if len(same) != 1000 {
		t.Fatalf("期望保留 1000 点，得到 %d", len(same))
	}

	// 2. 当请求 100 点时，下采样至 100 点
	ds := Downsample(samples, 100)
	if len(ds) != 100 {
		t.Fatalf("期望输出 100 点，得到 %d", len(ds))
	}

	// 3. 检查时间戳单调递增性
	for i := 1; i < len(ds); i++ {
		if ds[i].TS <= ds[i-1].TS {
			t.Errorf("时间戳未单调递增：ds[%d].TS=%d <= ds[%d].TS=%d", i, ds[i].TS, i-1, ds[i-1].TS)
		}
	}

	// 4. 检查桶内均值合理性
	if ds[0].Mem != 50.0 {
		t.Errorf("内存均值应为 50.0，得到 %f", ds[0].Mem)
	}
}
