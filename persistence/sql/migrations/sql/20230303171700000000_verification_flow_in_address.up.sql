ALTER TABLE
  identity_verifiable_addresses
ADD
  COLUMN "verification_flow_id" UUID;

-- ALTER TABLE
--   identity_verifiable_addresses
-- ADD
--   FOREIGN KEY (verification_flow_id) REFERENCES selfservice_verification_flows(id);