// /proof page copy (single-enclave architecture). NOTE: vue-i18n treats "@"
// and "|" as special syntax — a literal @ must be written {'@'}; digests and
// shell commands never go in here, the component renders those directly.
export default {
  proof: {
    header: {
      backHome: 'Home'
    },
    hero: {
      title: 'Follow your request — verify every step yourself.',
      subtitle:
        'This page shows where your request travels and what we can prove at each step. The whole service runs inside one hardware-protected confidential computer (an Intel TDX confidential VM), running open-source code anyone can audit line by line.',
      scopeNote:
        'Every verification below runs in your own browser against the chip vendor Intel’s root of trust — never through our servers. A step only turns green when all cryptographic checks pass.'
    },
    openSource: {
      title: 'Don’t trust us — read the code',
      body: 'This is not a black box. Every line running inside the confidential computer is open source and pinned to an exact version — anyone can audit the source, re-check it against the fingerprints published below, and confirm it matches this page’s live proof.',
      serviceLabel: 'Service source',
      serviceSub: 'Backend + frontend — all application code in the enclave',
      deployLabel: 'Deployment & reference',
      deploySub: 'The measured compose file and published fingerprints (reference.json)',
      ciLabel: 'Build pipeline',
      ciSub: 'Public CI: builds images, cosign signatures, SLSA provenance',
      viewSource: 'View source →',
      pinnedAt: "pinned {'@'} {commit}"
    },
    status: {
      pending: 'Pending',
      checking: 'Checking…',
      pass: 'Verified',
      fail: 'Failed',
      disclosure: 'Provider side'
    },
    journey: {
      title: 'The journey your request takes',
      basisLabel: 'Basis',
      hop1: {
        node: 'Your device → ApexOne gateway (running in confidential hardware)',
        claim:
          'Your request reaches a hardware-protected confidential computer (a confidential VM) over an encrypted connection; the code it runs is confirmed by checking its code fingerprint (compose_hash) — measured and signed by Intel hardware, impossible to fake. Want more tangible evidence? Use the end-to-end encryption demo below: your browser encrypts content directly to this computer’s dedicated key — only it can decrypt, and no middlebox sees plaintext.',
        basis: 'Code-fingerprint check + Intel hardware attestation (TDX)'
      },
      hop2: {
        node: 'ApexOne gateway (confidential hardware)',
        claim:
          'The confidential computer runs exactly the public, auditable code. The proof is fresh: your browser generates a one-time random number and the hardware binds it into the attestation — a replayed old report cannot fool you.',
        basis: 'One-time nonce binding + code fingerprint → public source version'
      },
      hop3: {
        node: 'ApexOne → upstream model providers',
        claim:
          'This step cannot be confidential: upstream providers (e.g. Anthropic, OpenAI) decrypt and process your plaintext under their own policies. We can only prove the request really came from the attested confidential computer.',
        basis: 'Honest disclosure — not a confidentiality promise'
      }
    },
    flow: {
      device: 'Your device',
      deviceSub: 'sends a request',
      teeLabel: 'ApexOne · Intel TDX confidential VM (hardware-isolated, memory-encrypted)',
      service: 'ApexOne TEE',
      serviceSub: 'request handled here',
      attestor: 'Attestor',
      attestorSub: 'issues live hardware proof',
      upstream: 'Upstream providers',
      upstreamSub: 'see plaintext (per their policies)',
      arrEncrypted: 'encrypted',
      arrInternal: 'same confidential VM',
      arrPlaintext: 'plaintext',
      legendConfidential: 'Protected: inside the confidential computer, neither the operator nor any middlebox can alter the code it runs',
      legendPlaintext: 'Plaintext: only the final step, handled by upstream providers under their privacy policies',
      caption:
        'In one sentence: the entire service runs in one hardware-protected, code-verifiable confidential computer — which code it runs, and that it is running right now, are both verifiable in your browser.'
    },
    disclosure: {
      title: 'Honest disclosure: upstream providers see plaintext',
      body:
        'Models are ultimately served by upstream providers (e.g. Anthropic, OpenAI), which necessarily process forwarded request content under their own policies. Your encrypted connection (TLS) terminates inside the confidential computer itself — the certificate private key is generated in the enclave and never leaves it, so no edge server outside the hardware boundary handles your plaintext. What we prove is that the confidential computer runs exactly the public, untampered code; we make no claim about what that code records for usage and billing, which is why the code is public.'
    },
    verify: {
      checksTitle: 'Per-check results',
      running: {
        title: 'Verifying in your browser…',
        note: 'Fetching the hardware attestation, checking the nonce binding, and verifying the quote against Intel’s official collateral (first run loads a ~426KB verifier).'
      },
      pass: {
        title: 'Hardware-level verification passed',
        note: 'This is genuine Intel confidential hardware, running exactly the code behind the published fingerprints, with the proof bound to this visit’s one-time nonce — freshly generated, not a replay. Honest boundary: the final upstream step sees plaintext by design.'
      },
      fail: {
        title: 'Verification FAILED',
        note: 'One or more gating checks did not pass. Do not trust this service — see the per-check results below.'
      },
      error: {
        title: 'Verification could not complete',
        note: 'Could not fetch the attestation or verification collateral (network / service temporarily unreachable). This is neither a pass nor a fail — please retry.'
      },
      lastVerified: 'Last verified at {time} UTC',
      rerun: 'Re-verify',
      checkLabel: {
        quote_present: 'Attestation payload integrity',
        quote_genuine: 'Genuine Intel confidential hardware',
        tcb_status: 'Hardware security patch level',
        nonce_binding: 'One-time nonce binding',
        measurement_rtmr3_replay: 'Measurement log authenticity (RTMR3 replay)',
        measurement_app_id: 'Application identity (app-id)',
        measurement_compose_hash_eventlog: 'Deployment fingerprint (event log)',
        measurement_compose_hash_mrconfigid: 'Deployment fingerprint (hardware register)',
        measurement_os_image_hash: 'OS image fingerprint',
        measurement_mrtd_reference: 'Raw firmware registers (informational)'
      }
    },
    live: {
      title: 'Read the hardware proof, live',
      desc:
        'Request a fresh hardware attestation from the confidential computer’s attestor right now, bound to a just-generated one-time random number (so nobody can fool you with an old report). Its hardware measurements are compared line-by-line against the published reference below. Key point: the report is only trustworthy after the quote is cryptographically verified — this page does that in your browser for you.',
      fetch: 'Fetch & verify proof',
      fetching: 'Fetching & verifying…',
      download: 'Download evidence (evidence.json)',
      nonce: 'One-time nonce',
      error: 'Could not reach the attestation service. It is served cross-origin by the confidential computer and requires CORS to be enabled.',
      endpointLabel: 'Attestation endpoint',
      collateralLabel: 'Intel verification collateral',
      explorerLabel: 'External verifier',
      referenceLabel: 'Published reference',
      refetch: 'Re-verify',
      autoNote: '💡 Verification runs automatically in your browser when this page opens; green check marks mean pass. You can re-run any time with a fresh nonce using the button above.',
      referenceSource: {
        repo: 'Reference fetched live from the public repository (history auditable in git)',
        bakedIn: 'Reference is the baked-in fallback copy (repository fetch failed — values may lag)'
      }
    },
    auditors: {
      title: 'For auditors',
      desc: 'Detailed evidence is collapsed by default to keep the page calm — expand any fold to verify line by line.',
      checks: 'Per-check results',
      checksPassed: '{n} passed',
      checksFailed: '{n} failed',
      checksInfo: '{n} informational',
      rawReport: 'Raw attestation payload',
      referenceHint: 'compare fingerprints line-by-line'
    },
    reference: {
      title: 'Published reference values (for deeper checking)',
      desc: 'These are the exact fingerprints of the confidential computer, from the public repository’s deploy/phala/reference.json and docs/attestation-verification.md. Any third party can compare them against the live attestation — matching values mean it really runs this public, untampered code.',
      enclave: 'ApexOne — confidential computer (single enclave)',
      appId: 'App ID',
      osImage: 'OS image',
      composeHash: 'Deployment fingerprint (compose_hash)',
      sourceCommit: 'Source version (commit)',
      serviceImage: 'Service image',
      attestorImage: 'Attestor image',
      ciNote: 'The images above are built and signed by this repository’s GitHub Actions (with verifiable provenance):'
    },
    e2ee: {
      title: 'End-to-end encryption proof',
      desc: 'The confidential computer publishes a dedicated encryption public key inside its hardware attestation (the private key is derived by the dstack key service inside the enclave and never leaves it). The hash of that key is bound into the TDX quote above — so "only it can decrypt" is vouched for by Intel hardware, not by our word.',
      statusVerified: 'Encryption key covered by the hardware attestation — verified in your browser',
      statusUnavailable: 'This deployment does not publish an E2EE key',
      statusIdle: 'Complete the hardware verification above first.',
      live: {
        title: 'See for yourself: only the enclave can decrypt (optional live test)',
        desc: 'An optional live test. Click below and your browser encrypts a prompt, sends it to the confidential computer’s REAL inference endpoint, then decrypts the encrypted model reply — showing you exactly who saw what, so you can confirm middleboxes only ever held ciphertext.',
        disclosure: 'Honest disclosure: this demo prompt is decrypted inside the confidential computer, then forwarded in plaintext to the upstream model provider by design, and consumes a small amount of quota (tokens). Do not paste anything sensitive. Your API key stays in your browser and is sent only with this one request for authorization. Want to check for yourself? Open your browser devtools, Network tab — the request body contains only ciphertext.',
        claimTitle: 'What this demo proves',
        claimBody: 'Your prompt is encrypted into ciphertext right in your browser, and only the hardware-attested confidential computer above can open it. The network and every middlebox on the way handle only that ciphertext — your content is unreadable to them, and this holds even if you assume TLS itself is broken or intercepted. That the enclave can decrypt it and encrypt a reply proves exactly this. (Note: after decryption the enclave forwards your prompt in plaintext to the upstream model provider — see the honest disclosure.)',
        sealedToLabel: 'Encrypted to (hardware-attested key, sha256)',
        concludeTitle: 'Conclusion',
        concludeBody: 'The receipt decrypts and authenticates ⟹ only the confidential computer holding the hardware-attested private key could open what you sent. The network and any middlebox cannot — and this holds regardless of whether you trust TLS.',
        stepsLabel: 'Technical step trace',
        apiKeyLabel: 'API key',
        apiKeyPlaceholder: 'sk-… (used only for this request, stays in your browser)',
        modelLabel: 'Model',
        promptLabel: 'Prompt',
        run: 'Encrypt & round-trip once',
        running: 'Encrypting & verifying…',
        successTitle: 'The attested confidential computer decrypted your ciphertext',
        successNote: 'Your prompt was encrypted in your browser to the enclave’s dedicated key, completed a real inference round-trip, and the encrypted reply passed authenticated decryption. No middlebox on the network path saw your plaintext — and that conclusion does not depend on trusting TLS.',
        promptSentLabel: '① Your message (plaintext — visible only to you and the enclave)',
        wireLabel: '② The actual bytes sent (all that the network and any middlebox see)',
        replyLabel: '③ Decrypted model reply (returned encrypted by the enclave)',
        failTitle: 'The live test could not complete',
        failNote: 'The request or decryption failed — raw error below; try again.',
        encryptedPrefix: 'Your prompt was encrypted and sent safely — that part passed. The request itself could not run:',
        unaffectedNote: 'The automatic hardware verification above is unaffected.',
        failure: {
          quota: { title: 'Test paused: insufficient balance', body: 'The account balance or quota is empty, so this request could not be billed and did not run.' },
          auth: { title: 'API key not accepted', body: 'The key you pasted is invalid or expired. Check it and retry.' },
          rate_limit: { title: 'Rate limited', body: 'Too many requests in a short time. Wait a moment and run it again.' },
          invalid_request: { title: 'The request could not be built', body: 'Check the model id above and make sure an API key is filled in, then retry.' },
          service: { title: 'Service temporarily unavailable', body: 'A server-side error was returned. Usually transient — try again later.' },
          network: { title: 'Could not reach the service', body: 'No response at all (network). This proves nothing either way — check connectivity and retry.' },
          trust: { title: 'Encrypted reply failed integrity check', body: 'The service replied, but the response could not be authenticated and decrypted with your client key. A real anomaly worth attention — see the step trace below.' }
        },
        seesTitle: 'Who saw what in this round-trip',
        youLabel: 'You (in your browser)',
        youSees: 'The plaintext message and the decrypted receipt',
        middleLabel: 'Network / any middlebox',
        middleSees: 'Only ciphertext — the content cannot be recovered',
        enclaveLabel: 'The hardware-attested confidential computer',
        enclaveSees: 'Can decrypt (and only it can) — which is why the echo works'
      }
    }
  }
}
