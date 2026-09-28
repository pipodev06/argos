-- +goose Up
-- Escalamiento de alertas: si nadie reconoce una alerta abierta en escalate_after_seconds, sube un nivel de
-- severidad y avisa a los canales de escalamiento de la regla (evento `escalation`). escalated_at marca que ya
-- escalo (una sola vez por alerta).
ALTER TABLE alert_rules ADD COLUMN escalate_after_seconds integer NULL
  CHECK (escalate_after_seconds IS NULL OR escalate_after_seconds BETWEEN 60 AND 604800);
ALTER TABLE alerts ADD COLUMN escalated_at timestamptz NULL;

CREATE TABLE rule_escalation_channels (
  rule_id UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, channel_id)
);
CREATE INDEX rule_escalation_channels_channel_idx ON rule_escalation_channels (channel_id);

ALTER TABLE notification_deliveries DROP CONSTRAINT notification_deliveries_event_check;
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_deliveries_event_check
  CHECK (event IN ('opened', 'resolved', 'reminder', 'escalation'));

-- +goose Down
DELETE FROM notification_deliveries WHERE event = 'escalation';
ALTER TABLE notification_deliveries DROP CONSTRAINT notification_deliveries_event_check;
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_deliveries_event_check
  CHECK (event IN ('opened', 'resolved', 'reminder'));
DROP TABLE rule_escalation_channels;
ALTER TABLE alerts DROP COLUMN escalated_at;
ALTER TABLE alert_rules DROP COLUMN escalate_after_seconds;
