-- demo-events-support.sql — Demo events for the support pipeline only.
--
-- Three complete ticket lifecycles across different categories:
--   customer-12345 — Shipping: delayed parcel (created → updated → resolved)
--   customer-67890 — Billing: double charge (created → updated → resolved)
--   department-IT  — Technical: VPN outage (created → updated → updated → resolved)
--
-- Idempotent: TRUNCATEs support data and streams first.
-- Run after demo-schema.sql and demo-data.sql.

TRUNCATE public.support_embeddings RESTART IDENTITY CASCADE;
TRUNCATE public.support_tickets    RESTART IDENTITY CASCADE;

DELETE FROM public.event_streams WHERE event_mapping_id = 2;

-- ─── Streams ──────────────────────────────────────────────────────────

INSERT INTO public.event_streams (stream, description, event_mapping_id, tag) VALUES
('customer-12345', 'Kunde 12345 — Lieferung verspätet',  2, 'Shipping'),
('customer-67890', 'Kunde 67890 — Doppelte Abrechnung',  2, 'Billing'),
('department-IT',  'IT-Abteilung — VPN-Störung',         2, 'Technical')
ON CONFLICT (stream) DO UPDATE SET
    description      = EXCLUDED.description,
    event_mapping_id = EXCLUDED.event_mapping_id,
    tag              = EXCLUDED.tag;

-- ─── customer-12345 — Shipping: delayed parcel ───────────────────────

INSERT INTO public.support_tickets (stream, event_type, text, payload) VALUES
('customer-12345', 'ticket_created',
 'Order #ORD-2024-8812 has not arrived after 10 days. Tracking DE123456789 shows shipment stuck in Frankfurt sorting centre since 2024-11-03. Customer contacted carrier twice without resolution.',
 '{"priority":"high","category":"shipping","tracking":"DE123456789","amount":"89.95"}'),

('customer-12345', 'ticket_updated',
 'Contacted DHL logistics team. Parcel located in Frankfurt hub — damaged label caused routing failure. Parcel re-labelled and dispatched. Estimated delivery 2024-11-07.',
 '{"status":"in_progress","agent":"Sarah Mueller","note":"DHL confirmed re-routing, ETA 2 days"}'),

('customer-12345', 'ticket_resolved',
 'Customer confirmed parcel received on 2024-11-07. Offered 10% discount voucher for inconvenience. Customer accepted and expressed satisfaction.',
 '{"resolution":"Parcel delivered after re-routing by carrier","resolved_by":"Sarah Mueller","refund":"voucher_10pct"}');

-- ─── customer-67890 — Billing: double charge ────────────────────────

INSERT INTO public.support_tickets (stream, event_type, text, payload) VALUES
('customer-67890', 'ticket_created',
 'Customer reports being charged twice for the premium subscription on 2024-11-01. Bank statement shows two debit entries of EUR 29.99 on the same day. Requests immediate refund of the duplicate charge.',
 '{"priority":"medium","category":"billing","amount":"29.99"}'),

('customer-67890', 'ticket_updated',
 'Confirmed in billing system: payment gateway retried due to timeout, resulting in a duplicate charge. Refund of EUR 29.99 initiated. Processing time 3–5 business days.',
 '{"status":"in_progress","agent":"Jonas Weber","note":"Duplicate confirmed, refund initiated via Stripe"}'),

('customer-67890', 'ticket_resolved',
 'Customer confirmed refund of EUR 29.99 received. Root cause: payment gateway timeout retry without idempotency key. Engineering team notified to implement fix.',
 '{"resolution":"Refund processed, root cause escalated to engineering","resolved_by":"Jonas Weber","refund":"29.99"}');

-- ─── department-IT — Technical: VPN outage ───────────────────────────

INSERT INTO public.support_tickets (stream, event_type, text, payload) VALUES
('department-IT', 'ticket_created',
 'VPN connection broken for all Windows 11 users after KB5031455 update rolled out on 2024-11-04. 15 employees unable to access internal systems. MacOS users unaffected. Business-critical: finance team cannot access ERP.',
 '{"priority":"high","category":"network","affected_users":15}'),

('department-IT', 'ticket_updated',
 'Identified root cause: KB5031455 breaks WireGuard TAP adapter on Windows 11 22H2. Microsoft acknowledged bug in support ticket MSC-2024-98123. Workaround: roll back update via WSUS. Rollback in progress for affected machines.',
 '{"status":"in_progress","agent":"Klaus Bauer","note":"Rollback via WSUS started, 8 of 15 machines restored"}'),

('department-IT', 'ticket_updated',
 'All 15 machines rolled back. VPN connectivity restored for all users. Finance team confirmed ERP access working. Monitoring for 24h before closing. WSUS policy updated to defer KB5031455 until Microsoft patch available.',
 '{"status":"in_progress","agent":"Klaus Bauer","note":"All machines restored, monitoring active","affected_users":0}'),

('department-IT', 'ticket_resolved',
 'No recurrence after 24h monitoring. Ticket closed. Permanent fix: WSUS deferral rule applied, endpoint policy updated. Internal knowledge base article published for future reference.',
 '{"resolution":"Update rolled back, WSUS deferral policy applied","resolved_by":"Klaus Bauer"}');

-- Demonstrate closed events: a resolved ticket is, by definition,
-- closed. Marks the final ticket_resolved entry of each ticket as
-- closed so the lifecycle trigger guards them from further edits.
UPDATE public.support_tickets
   SET closed    = true,
       closed_at = now()
 WHERE event_type = 'ticket_resolved';

ALTER TABLE public.support_tickets
    VALIDATE CONSTRAINT support_tickets_stream_fkey;
