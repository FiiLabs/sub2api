# ApexOne Privacy Policy

**Last updated: 2026-07-09**

This Privacy Policy (this "Policy") describes how ApexOne ("ApexOne," "we," "our," or "us") collects, uses, discloses, and protects information in connection with the ApexOne service. ApexOne is a verifiable AI gateway that helps individual users access frontier AI models through a single OpenAI-compatible API, routed through an attested Trusted Execution Environment so that the routing layer's privacy boundary can be independently verified. This Policy applies to your use of the ApexOne website, applications, application programming interfaces, and associated products (collectively, the "Services"). By accessing or using the Services, you ("User," "you," or "your") acknowledge that you have read and agree to this Policy and the ApexOne Terms of Service.

This Policy governs how we handle personal information. Contractual matters — including dispute resolution, indemnification, limitation of liability, and force majeure — are governed exclusively by the ApexOne Terms of Service and are not duplicated here.

## 1. Scope

This Policy applies to all Services offered under the ApexOne brand, including services provided through third-party sites such as analytics and infrastructure providers. It does not apply to services governed by a separate privacy policy that does not incorporate this Policy by reference.

This Policy does not apply to:

- the information practices of other companies or organizations that advertise or link to the Services; or

- services offered by other companies or individuals, including the external AI provider APIs and upstream model providers accessed through the Services.

Where you use ApexOne to route requests to an upstream AI provider, that provider's own privacy policy governs how it processes the data it receives. ApexOne does not control, and is not responsible for, the data practices of upstream providers.

ApexOne acts as the controller of the personal information it processes in connection with the Services, except where an upstream provider independently determines its own processing of the data you send to it.

## 2. Information We Collect

- **Automatically collected information.** We use third-party tools that collect information through cookies for analytics and communication purposes. This includes cookies used to store User preferences, IP addresses, browser user-agent strings, and request header information collected for anti-spam and abuse-prevention purposes.

- **Account information.** Email addresses may be used to identify and authenticate Users.

- **Payment information.** Payments are processed through third-party payment platforms that may retain identifying details. ApexOne does not store your full financial account information on its servers.

- **Usage metadata.** To operate routing and billing, ApexOne records usage metadata, including the requesting User, the model and provider used, token counts, cost, latency, request status, and timestamps. This metadata is used to deliver usage visibility and to produce billing records.

### TEE-based privacy protection

The ApexOne gateway runs inside an Intel TDX confidential virtual machine (a "Trusted Execution Environment" or "TEE"). Routing of your requests occurs entirely within this attested environment:

- **Data in transmission.** Prompts, inputs, and outputs are encrypted in transit, and the ApexOne routing layer outside the TEE cannot access the plaintext content.

- **Data in use.** Routing computation occurs within the TEE, and ApexOne does not access the memory, CPU state, or runtime data of the protected routing layer.

- **Data in storage.** Any temporary data within the TEE is encrypted and isolated from general ApexOne infrastructure.

- **Prompt and response privacy.** ApexOne does not collect, log, or store the plaintext content of your prompts or responses. Your prompts are decrypted only inside the attested TEE for the purpose of routing them to the selected upstream provider, and remain confidential to ApexOne's operators, logs, and intermediate infrastructure.

When ApexOne forwards your request to an upstream AI provider to fulfill it, that upstream provider receives the request and response content in plaintext and processes it under its own terms and privacy policy. This is the boundary of ApexOne's guarantee: the privacy boundary covers ApexOne's own gateway and routing layer, not the upstream provider's handling of the data you choose to send to it.

## 3. Purposes of Processing

ApexOne collects the minimum data necessary to operate and improve the Services. The purposes of processing include:

- **Delivering the Services** — routing requests to upstream AI providers and providing unified, OpenAI-compatible API access.

- **Maintaining service integrity** — detecting spam, abuse, fraud, or downtime.

- **Improving routing and reliability** — measuring provider performance to improve stability and availability.

- **Communicating with Users** — regarding critical updates, service changes, and billing.

- **Complying with legal obligations** — and preventing fraud.

## 4. Verifiable Privacy and Remote Attestation

The Services are designed so that privacy protection is technically verifiable rather than dependent on a policy promise alone. ApexOne provides remote attestation evidence that you can check directly from your browser (for example, on the ApexOne proof page), enabling you to verify that the gateway is running the expected, privacy-preserving configuration inside the attested TEE and cannot access the plaintext of your prompts or responses.

The scope of this attestation covers the ApexOne gateway, its measured code identity, and its attested encryption keys. It does not cover, and ApexOne does not represent that it can verify, the model weights, model identity, or internal behavior of any upstream AI provider. ApexOne does not represent that upstream model providers are unable to access the data you choose to send to them.

## 5. Data Retention

ApexOne retains personal information only for as long as necessary for the purposes described in this Policy. The following retention periods apply unless a longer period is required to comply with law, resolve disputes, or enforce our agreements:

| Data category | Retention period |
|---|---|
| Plaintext prompt and response content | Not retained |
| Usage metadata (user, model, provider, tokens, cost, latency, status, timestamp) | Up to 24 months, then deleted or aggregated |
| Account identifiers | Duration of the account, then deleted within 90 days of account closure |
| Billing and transaction records | As required by applicable tax and accounting law (typically 7 years) |
| IP and security logs | Up to 6 months for anti-spam and abuse prevention |
| Support communications | Up to 24 months after the matter is resolved |

Plaintext prompt and response content is not retained by ApexOne under any circumstances.

## 6. Your Privacy Rights

Depending on your location (including under the EU/UK GDPR, the California Consumer Privacy Act as amended by the CPRA, and similar laws), you may have the right to:

- access the personal information we hold about you and obtain a copy;

- correct inaccurate or incomplete personal information;

- delete your personal information;

- port your personal information to another service in a structured, machine-readable format;

- restrict or object to certain processing;

- withdraw consent where processing is based on consent; and

- not be discriminated against for exercising these rights.

ApexOne does not sell or "share" personal information for cross-context behavioral advertising as those terms are defined under California law.

**How to exercise your rights.** You may submit a request by contacting us at support@apex1.us. We will verify your identity and respond within the timeframe required by applicable law (for example, 45 days under the CCPA, extendable as permitted, and one month under the GDPR). You may use an authorized agent where the law allows.

**Appeals.** Where required by law, if we decline a request you may appeal by contacting us at support@apex1.us; we will respond within the period the applicable law prescribes. You may also lodge a complaint with your local supervisory authority or regulator.

## 7. International Data Transfers

ApexOne and its service providers may process personal information in countries other than the one in which you reside, including the United States. Where we transfer personal information across borders, we rely on an appropriate transfer mechanism, which may include the European Commission's Standard Contractual Clauses, the UK International Data Transfer Addendum, an adequacy decision, or another lawful mechanism. A copy of the relevant safeguards is available on request at support@apex1.us. Plaintext prompt and response content is not accessible to ApexOne in any location.

## 8. Disclosure of Information

ApexOne does not sell, lease, or trade your personal information.

ApexOne may disclose information only in the following circumstances:

- with your explicit consent;

- to trusted sub-processors and service providers operating under contractual obligations, as described in Section 9;

- to the upstream AI providers necessary to fulfill your routed requests;

- to comply with law enforcement or regulatory requirements; and

- in connection with a business reorganization or transfer, with notice.

## 9. Sub-Processors

ApexOne engages third-party sub-processors to provide the Services, including infrastructure and hosting providers, TEE hardware providers, payment processors, analytics providers, and communication providers. ApexOne imposes data protection obligations on each sub-processor that are no less protective than those in this Policy. A current list of sub-processors, including each sub-processor's name, function, and processing location, is available on request at support@apex1.us.

## 10. Security and Breach Notification

ApexOne maintains administrative, technical, and organizational measures designed to protect personal information, including encryption in transit, encryption of stored secrets, access controls, and TEE-based isolation for the routing layer.

If ApexOne becomes aware of a personal data breach affecting your personal information, we will notify affected Users without undue delay and, where required by applicable law, within the legally mandated timeframe (for example, 72 hours of becoming aware, under the GDPR, for notification to the relevant authority). Our notice will describe, to the extent known, the nature of the breach, the categories of data affected, the likely consequences, and the measures taken or proposed. This is in addition to your obligation to notify us promptly of any security issue you suspect.

## 11. Children's Privacy

The Services are not directed to children. You must meet the minimum age requirements set out in the ApexOne Terms of Service to use the Services. ApexOne does not knowingly collect personal information from children below the applicable minimum age. If we learn that we have collected such information without required parental consent, we will delete it.

## 12. Third-Party Services

The Services connect to external AI model APIs and infrastructure providers, which are governed by their own terms. ApexOne does not guarantee the privacy, accuracy, or availability of outputs from third-party sources.

## 13. Governing Law

This Policy shall be governed by and construed in accordance with the laws of the State of California, without regard to its conflict-of-law principles. This governing-law provision concerns the interpretation of this Policy only; dispute resolution between you and ApexOne is governed by the ApexOne Terms of Service.

## 14. Contact

If you have questions or requests relating to this Policy, or wish to exercise your privacy rights, please contact us at support@apex1.us.

## 15. Changes to this Policy

ApexOne may update this Policy from time to time and will indicate the date of the latest revision. If changes are material, ApexOne will provide a more prominent notice, which may include notification by email. Your continued use of the Services after any update constitutes acceptance of the revised Policy. If you do not agree to the changes, you must discontinue use of the Services.
