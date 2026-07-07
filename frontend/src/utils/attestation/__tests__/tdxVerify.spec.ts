/**
 * Tests for item 2b — TDX DCAP hardware-quote verification (`tdxVerify.ts`).
 *
 * The report_data binding, hex/byte logic, dstack event-digest algorithm and
 * RTMR3 replay are all tested DETERMINISTICALLY against a captured live gateway
 * quote (`tdxFixture.json`) — no network. The full `verifyQuoteBoundToReport`
 * path (wasm + Phala PCCS collateral) is exercised in an integration test that
 * skips automatically when PCCS is unreachable.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, it, expect } from 'vitest'
import { GATEWAY_REFERENCE } from '@/constants/attestation'
import type { AttestationReport } from '@/api/attestation'
import {
  expectedAciReportDataHex,
  checkReportDataBinding,
  dstackEventDigestHex,
  replayRtmrHex,
  extractRegisters,
  verifyQuoteBoundToReport,
  __setWasmInputForTests,
  PHALA_PCCS_URL,
} from '../tdxVerify'

import fixture from './tdxFixture.json'

// TD10 register byte-offsets within the raw quote (48-byte header + TD report).
const H = 48
const OFF = { mr_config_id: H + 184, rt_mr3: H + 472, report_data: H + 520 }
const quoteBytes = Buffer.from(fixture.evidence.quote, 'hex')
const sliceHex = (o: number, l: number) => quoteBytes.subarray(o, o + l).toString('hex')
const QUOTE_RT_MR3 = sliceHex(OFF.rt_mr3, 48)
const QUOTE_MR_CONFIG_ID = sliceHex(OFF.mr_config_id, 48)
const QUOTE_REPORT_DATA = sliceHex(OFF.report_data, 64)

interface FixtureEvent {
  imr: number
  event_type: number
  digest: string
  event: string
  event_payload: string
}
const events: FixtureEvent[] = JSON.parse(fixture.evidence.event_log)

const DIGEST = 'a'.repeat(64) // synthetic 32-byte ACI digest

describe('expectedAciReportDataHex', () => {
  it('appends 32 zero bytes to the digest', () => {
    expect(expectedAciReportDataHex(DIGEST)).toBe(DIGEST + '00'.repeat(32))
  })
  it('normalizes a 0x prefix', () => {
    expect(expectedAciReportDataHex('0x' + DIGEST)).toBe(DIGEST + '00'.repeat(32))
  })
  it('rejects a wrong-length digest', () => {
    expect(() => expectedAciReportDataHex('abcd')).toThrow()
  })
})

describe('checkReportDataBinding', () => {
  it('accepts digest || zeros', () => {
    const r = checkReportDataBinding(DIGEST + '00'.repeat(32), DIGEST)
    expect(r.ok).toBe(true)
  })
  it('rejects a non-zero tail', () => {
    const r = checkReportDataBinding(DIGEST + '00'.repeat(31) + '01', DIGEST)
    expect(r.ok).toBe(false)
  })
  it('rejects a report_data that is not 64 bytes', () => {
    expect(checkReportDataBinding(DIGEST, DIGEST).ok).toBe(false)
  })
  it('rejects the wrong digest', () => {
    const r = checkReportDataBinding(DIGEST + '00'.repeat(32), 'b'.repeat(64))
    expect(r.ok).toBe(false)
  })
  it('rejects the live legacy-format report_data (signing-address, non-zero tail)', () => {
    // The captured quote uses the legacy report_data layout
    // (eth-address || zeros || hash), NOT the aci/1 digest||zeros model, so the
    // ACI binding correctly fails against it. Documents observed reality.
    const digest = QUOTE_REPORT_DATA.slice(0, 64)
    expect(checkReportDataBinding(QUOTE_REPORT_DATA, digest).ok).toBe(false)
  })
})

describe('dstackEventDigestHex', () => {
  it('reproduces the recorded digest for every RTMR3 event (binds payloads)', () => {
    const imr3 = events.filter((e) => e.imr === 3)
    expect(imr3.length).toBeGreaterThan(0)
    for (const ev of imr3) {
      expect(dstackEventDigestHex(ev), `event=${ev.event}`).toBe(ev.digest.toLowerCase())
    }
  })
})

describe('replayRtmrHex', () => {
  it('replays the event log into the quote rt_mr3 register', () => {
    expect(replayRtmrHex(events, 3)).toBe(QUOTE_RT_MR3)
  })
})

describe('measurement reference policy (from event log + mr_config_id)', () => {
  const payload = (name: string) =>
    events.find((e) => e.imr === 3 && e.event === name)?.event_payload.toLowerCase()

  it('compose-hash event matches the gateway reference', () => {
    expect(payload('compose-hash')).toBe(GATEWAY_REFERENCE.composeHash.toLowerCase())
  })
  it('mr_config_id[1..33] carries the same compose_hash', () => {
    expect(QUOTE_MR_CONFIG_ID.slice(2, 66)).toBe(GATEWAY_REFERENCE.composeHash.toLowerCase())
  })
  it('os-image-hash event matches the gateway reference', () => {
    expect(payload('os-image-hash')).toBe(GATEWAY_REFERENCE.osImageHash.toLowerCase())
  })
  it('app-id event matches the gateway reference', () => {
    expect(payload('app-id')).toBe(GATEWAY_REFERENCE.appId.toLowerCase())
  })
})

describe('extractRegisters', () => {
  it('normalizes a TD10 report', () => {
    const reg = extractRegisters({
      TD10: {
        mr_td: 'AA',
        mr_config_id: 'BB',
        rt_mr0: '00',
        rt_mr1: '11',
        rt_mr2: '22',
        rt_mr3: '33',
        report_data: 'CC',
      },
    })
    expect(reg.kind).toBe('TD10')
    expect(reg.mrTdHex).toBe('aa')
    expect(reg.rtMr3Hex).toBe('33')
    expect(reg.reportDataHex).toBe('cc')
  })
  it('unwraps a TD15 report via base', () => {
    const reg = extractRegisters({
      TD15: {
        base: {
          mr_td: 'aa',
          mr_config_id: 'bb',
          rt_mr0: '',
          rt_mr1: '',
          rt_mr2: '',
          rt_mr3: 'ff',
          report_data: 'cc',
        },
      },
    })
    expect(reg.kind).toBe('TD15')
    expect(reg.rtMr3Hex).toBe('ff')
  })
})

// --- integration (network to Phala PCCS + wasm) -----------------------------

async function pccsReachable(): Promise<boolean> {
  try {
    await fetch(PHALA_PCCS_URL, { signal: AbortSignal.timeout(5000) })
    return true
  } catch {
    return false
  }
}

describe('verifyQuoteBoundToReport (integration, needs network)', () => {
  it('verifies the live TD10 quote end-to-end', async () => {
    if (!(await pccsReachable())) {
      console.warn('[tdxVerify.spec] Phala PCCS unreachable — skipping integration test')
      return
    }
    // Seed the wasm bytes so init works under Node (browser uses ?url).
    const wasm = readFileSync(
      resolve(process.cwd(), 'node_modules/@phala/dcap-qvl-web/dcap-qvl-web_bg.wasm'),
    )
    __setWasmInputForTests(wasm)

    const report = {
      attestation: {
        tee_type: fixture.tee_type,
        report_data: fixture.attestation_report_data,
        evidence: fixture.evidence,
      },
    } as unknown as AttestationReport

    // Use the digest that the legacy quote actually carries so we can also see
    // the binding check; freshness of collateral is "now".
    const digest = QUOTE_REPORT_DATA.slice(0, 64)
    const res = await verifyQuoteBoundToReport(report, digest, { reference: GATEWAY_REFERENCE })

    const byId = Object.fromEntries(res.checks.map((c) => [c.id, c]))
    // Genuine hardware + TCB.
    expect(byId.quote_genuine?.ok, JSON.stringify(byId.quote_genuine)).toBe(true)
    expect(byId.tcb_status?.ok, JSON.stringify(byId.tcb_status)).toBe(true)
    expect(byId.tee_type?.ok).toBe(true)
    // Full measurement policy verified via the audited wasm-parsed registers.
    expect(byId.measurement_rtmr3_replay?.ok).toBe(true)
    expect(byId.measurement_event_digest_binding?.ok).toBe(true)
    expect(byId.measurement_compose_hash_eventlog?.ok).toBe(true)
    expect(byId.measurement_compose_hash_mrconfigid?.ok).toBe(true)
    expect(byId.measurement_os_image_hash?.ok).toBe(true)
    expect(byId.measurement_app_id?.ok).toBe(true)
    // Raw MRTD has no reference: explicitly non-gating + unverified.
    expect(byId.measurement_mrtd_reference?.ok).toBe(false)
    expect(byId.measurement_mrtd_reference?.gating).toBe(false)
    // mr_config_id compose_hash surfaced in measurements.
    expect(res.measurements.composeHashFromConfigId).toBe(
      GATEWAY_REFERENCE.composeHash.toLowerCase(),
    )
    expect(res.measurements.tcbStatus).toBe('UpToDate')
    __setWasmInputForTests(undefined)
  }, 30000)
})
