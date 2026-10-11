# Privacy model

The participant-facing notice is `web/app/privacy/page.tsx` (version `v1`; enrollment requires `consent: "v1"`). This is the technical inventory behind it.

| Data | Where | Retention | Public? |
|---|---|---|---|
| Raw samples | server memory: 60 s ring per connected device | discarded continuously | no |
| Event snippets (≈ 55 s of raw motion from phones that triggered) | `snippets` table | 365 days | only as envelopes averaged per 1 km distance ring |
| Picks + features | `picks` table, with device ID and 1 km cell | 180 days | no (operators only) |
| Device record: random ID, SHA-256(secret), platform, join time, last seen, 1 km cell, totals | `devices` table | deleted on request or after 180 days idle | no |
| 1 km cell | computed on the phone from GPS or a map tap; coordinates are discarded on the phone | with the device | aggregated to 10 km cells, hidden below 3 phones |
| IP address | memory only (rate limits); a keyed hash of the /24 or /48 prefix with a daily key for source diversity | not stored | no |
| Events | `events` table | indefinitely (scientific record) | yes, at 10 km resolution |
| Class recordings (only devices that joined with a registered class code): continuous raw samples | gzip CSV files under `-record-dir`, per class/day/device-hour, device named by a keyed pseudonym | `-record-days` (default 365); deleted with the device | no (admin download only) |
| Class code | `devices.cohort` | with the device | no |
| Telegram subscription: chat ID, 10 km area, language | `subscribers` table | until `/stop` in the bot, or the user blocks the bot | no |

Deletion (`DELETE /api/v1/devices/me`) removes the device row, its picks and its snippets in one transaction, deletes its class recordings, and closes its session. Events keep aggregate counts only.

Research datasets exported for publication must drop `device_id` (or replace it with a per-export random key) and keep locations at ≥ 1 km.

**Minors.** Most planned participants are school students under 18. The system collects no names, but class recording is continuous and linked to a class code, so it must only start after the school has approved the study and parents have received the information sheet in `docs/consent/`. The join page asks the participant to confirm this before a code is accepted.

**Ethics.** If the research is done through a university, participant data collection may require ethics or IRB review before a public recruitment drive (for example a large Telegram group). The consent flow and notice are written to support that, but only the institution can grant approval.
