package history

// Downsample 将采样切片降维聚合到最多 maxPoints 个代表点。
// 当原始采样数小于等于 maxPoints 时直接返回原切片；
// 超过时将数据按时间连续切分成等宽桶（Buckets），计算桶内均值输出，
// 从而大幅减轻长时历史查询的传输体积和前端图表渲染负担。
func Downsample(samples []Sample, maxPoints int) []Sample {
	n := len(samples)
	if n <= maxPoints || maxPoints <= 0 {
		return samples
	}

	out := make([]Sample, 0, maxPoints)
	bucketSize := float64(n) / float64(maxPoints)

	for b := 0; b < maxPoints; b++ {
		start := int(float64(b) * bucketSize)
		end := int(float64(b+1) * bucketSize)
		if end > n {
			end = n
		}
		if start >= end {
			continue
		}

		count := float32(end - start)
		var (
			sumCPU     float32
			sumMem     float32
			sumMemUsed float64
			sumSwap    float32
			sumLoad1   float32
			sumBrk     [6]float32
			sumPerCore [perCoreSlots]float32
			sumNetRx   float64
			sumNetTx   float64
			sumIOR     float64
			sumIOW     float64
		)

		for i := start; i < end; i++ {
			s := samples[i]
			sumCPU += s.CPU
			sumMem += s.Mem
			sumMemUsed += s.MemUsed
			sumSwap += s.Swap
			sumLoad1 += s.Load1
			for j := 0; j < 6; j++ {
				sumBrk[j] += s.Brk[j]
			}
			for j := 0; j < perCoreSlots; j++ {
				sumPerCore[j] += s.PerCore[j]
			}
			sumNetRx += s.NetRx
			sumNetTx += s.NetTx
			sumIOR += s.IOR
			sumIOW += s.IOW
		}

		mid := (start + end - 1) / 2
		rep := Sample{
			TS:      samples[mid].TS,
			CPU:     sumCPU / count,
			Mem:     sumMem / count,
			MemUsed: sumMemUsed / float64(count),
			Swap:    sumSwap / count,
			Load1:   sumLoad1 / count,
			NetRx:   sumNetRx / float64(count),
			NetTx:   sumNetTx / float64(count),
			IOR:     sumIOR / float64(count),
			IOW:     sumIOW / float64(count),
		}
		for j := 0; j < 6; j++ {
			rep.Brk[j] = sumBrk[j] / count
		}
		for j := 0; j < perCoreSlots; j++ {
			rep.PerCore[j] = sumPerCore[j] / count
		}

		out = append(out, rep)
	}

	return out
}
