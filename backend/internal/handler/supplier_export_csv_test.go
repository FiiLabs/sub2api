//go:build unit

// APEXONE-EXT: 对账导出 CSV 编码的单元测试。
//
// 这些性质全都只有在**别人的电脑上打开这个文件时**才会显现，所以必须在这里钉死：
// 表格软件把某个单元格当成了公式、中文成了乱码、文件少了一半而运营不知道。
// 三样都不会让任何一条服务端日志变红。
package handler

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 公式注入：这是这个文件里唯一一条安全性质。
//
// user_note 与 payout_account 由**供给者**填写，而这份文件由运营在自己的机器上
// 用 Excel 打开。一个以 = 开头的单元格在那里是可执行的。
func TestCSVTextNeutralizesFormulaTriggers(t *testing.T) {
	for name, input := range map[string]string{
		"等号":  "=HYPERLINK(\"http://evil\",\"click\")",
		"加号":  "+1+1",
		"减号":  "-2+3+cmd|' /c calc'!A0",
		"@":   "@SUM(A1:A9)",
		"制表符": "\tinjected",
		"回车":  "\rinjected",
	} {
		t.Run(name, func(t *testing.T) {
			got := csvText(input)
			assert.True(t, strings.HasPrefix(got, "'"),
				"%q 没有被中和，它会在运营的表格软件里被当成公式：%q", input, got)
			assert.Equal(t, input, strings.TrimPrefix(got, "'"), "中和不该改动内容本身")
		})
	}
}

func TestCSVTextLeavesOrdinaryValuesAlone(t *testing.T) {
	for _, ordinary := range []string{
		"supplier@example.com",
		"6222 0202 0001 2345 678 / 张三 / 招商银行深圳分行",
		"USDT",
		"这个月的第一笔",
		"paid",
	} {
		assert.Equal(t, ordinary, csvText(ordinary))
	}
	assert.Equal(t, "", csvText(""), "空值加了引号就不再是空单元格了")
}

// 金额列**不**走 csvText。
//
// 走了的话，任何一个负数（追回、退回那几种流水）会变成 `'-1.5`，
// 于是整列在表格软件里变成文本：求和求不出来，排序按字典序。
// 把一个安全问题换成一个对账问题不划算，而金额本来就不可能被注入——
// 它来自 NUMERIC 列，不是用户输入。
func TestLedgerCSVRowKeepsNegativeAmountsNumeric(t *testing.T) {
	row := supplyLedgerCSVRow(&service.SupplyLedgerExportRow{
		ID: 1, UserID: 2, Action: "clawback",
		Amount: "-1.50000000", BasisAmount: "-3.00000000",
		AvailableAfter: "-0.10000000",
	})
	assert.Contains(t, row, "-1.50000000")
	for _, cell := range row {
		assert.False(t, strings.HasPrefix(cell, "'-"), "金额被当成文本转义了：%q", cell)
	}
}

// 表头与数据行的列数必须一致。
//
// 错开一列的表现不是报错，是**每一行的每个值都挪了一格**：运营看到的是一份
// 收款账号列里写着备注、金额列里写着渠道名的表格。
func TestExportRowsMatchTheirHeaders(t *testing.T) {
	assert.Len(t, supplierWithdrawalCSVRow(&service.SupplierWithdrawalExportRow{}),
		len(supplierWithdrawalCSVHeader), "提现导出的列数与表头对不上")
	assert.Len(t, supplyLedgerCSVRow(&service.SupplyLedgerExportRow{}),
		len(supplyLedgerCSVHeader), "流水导出的列数与表头对不上")
}

// 缺失的 id 是空单元格，不是 0 号账号。
func TestCSVIDBlanksZero(t *testing.T) {
	assert.Equal(t, "", csvID(0))
	assert.Equal(t, "7", csvID(7))
}

// 时间一律 UTC 的 RFC3339；零值是空单元格。
func TestCSVTimeIsUTCRFC3339(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	local := time.Date(2026, 8, 20, 17, 30, 0, 0, shanghai)

	assert.Equal(t, "2026-08-20T09:30:00Z", csvTime(local),
		"没转成 UTC：一份跨时区流转的对账文件说不清这是谁的下午五点半")
	assert.Equal(t, "", csvTime(time.Time{}))
	assert.Equal(t, "", csvTimePtr(nil))
}

// BOM 必须正好是那三个字节。
//
// 少了 Windows 版 Excel 会按本地代码页解码，收款账号里的开户人姓名成乱码；
// 多了或错了则会作为一个可见的怪字符出现在第一个表头单元格里。
func TestUTF8BOMIsExactlyThreeBytes(t *testing.T) {
	assert.Equal(t, []byte{0xEF, 0xBB, 0xBF}, []byte(utf8BOM))
}

// ============================================================================
// 尾行
// ============================================================================

func TestExportTrailerReportsRowCountAndWindow(t *testing.T) {
	window := service.SupplyExportWindow{
		StartAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	trailer := supplyExportTrailer(service.SupplyExportOutcome{Rows: 42}, window)

	require.Len(t, trailer, 2)
	assert.Equal(t, "#", trailer[0], "尾行第一格必须是固定前缀，脚本靠它判定文件写完了")
	assert.Contains(t, trailer[1], "42 rows")
	assert.Contains(t, trailer[1], "2026-07-01T00:00:00Z")
	assert.Contains(t, trailer[1], "2026-08-01T00:00:00Z")
	assert.NotContains(t, trailer[1], "TRUNCATED")
}

// 撞了行数上限必须在文件里说出来。
//
// 这是整条导出链路上最要紧的一条断言。响应头早在第一行数据之前就发出去了，
// 状态码改不了——尾行是唯一能告诉运营"这份文件不完整"的地方。
// 少了它，他拿到的是一个"下载成功"的、少了一半账的对账文件，然后照着它打款。
func TestExportTrailerShoutsWhenTruncated(t *testing.T) {
	trailer := supplyExportTrailer(
		service.SupplyExportOutcome{Rows: service.SupplyExportMaxRows, Truncated: true},
		service.SupplyExportWindow{},
	)
	require.Len(t, trailer, 2)
	assert.Contains(t, trailer[1], "TRUNCATED", "截断了却没在文件里说")
	assert.Contains(t, trailer[1], "narrow the time range",
		"只说截断不说怎么办，运营下一步还是只能再点一次同样的按钮")
}

// ============================================================================
// 骨架
// ============================================================================

func TestWriteSupplyExportCSVAlwaysEndsWithTrailer(t *testing.T) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)

	err := writeSupplyExportCSV(writer, []string{"a", "b"}, service.SupplyExportWindow{},
		func(write func([]string) error) (service.SupplyExportOutcome, error) {
			require.NoError(t, write([]string{"1", "2"}))
			require.NoError(t, write([]string{"3", "4"}))
			return service.SupplyExportOutcome{Rows: 2}, nil
		})
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n")
	require.Len(t, lines, 4, "表头 + 两行数据 + 尾行")
	assert.Equal(t, "a,b", lines[0])
	assert.True(t, strings.HasPrefix(lines[3], "#"), "最后一行不是尾行：%q", lines[3])
}

// 中途出错时**不写**尾行——这是残缺文件唯一的可辨识特征。
//
// 状态码已经是 200 了，改不了；写一行尾行等于给一份半截文件盖上"完整"的章。
func TestWriteSupplyExportCSVOmitsTrailerOnMidStreamError(t *testing.T) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	boom := errors.New("connection reset by peer")

	err := writeSupplyExportCSV(writer, []string{"a"}, service.SupplyExportWindow{},
		func(write func([]string) error) (service.SupplyExportOutcome, error) {
			require.NoError(t, write([]string{"1"}))
			return service.SupplyExportOutcome{Rows: 1}, boom
		})
	require.ErrorIs(t, err, boom)

	writer.Flush()
	assert.NotContains(t, buffer.String(), "#",
		"出错了还是把尾行写了：运营会以为这份半截文件是完整的")
}
