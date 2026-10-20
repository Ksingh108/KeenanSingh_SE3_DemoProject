-- +goose Up
INSERT INTO branches (id, name, address, city, phone, hours, capacity) VALUES
('550e8400-e29b-41d4-a716-446655440001', 'Diagon Alley Main', '93 Diagon Alley', 'Johannesburg', '+27 11 555 0901', 'Mon-Fri 9:00-17:00, Sat 9:00-13:00', 15),
('550e8400-e29b-41d4-a716-446655440002', 'Knockturn Alley', '12 Knockturn Alley', 'Johannesburg', '+27 11 555 0902', 'Mon-Fri 10:00-16:00', 8),
('550e8400-e29b-41d4-a716-446655440003', 'Hogsmeade Village', '1 High Street', 'Cape Town', '+27 21 555 0903', 'Mon-Sat 9:00-17:00', 12)
ON CONFLICT (id) DO NOTHING;

INSERT INTO services (id, name, description) VALUES
('660e8400-e29b-41d4-a716-446655440001', 'Account Opening', 'Open new vault accounts'),
('660e8400-e29b-41d4-a716-446655440002', 'Vault Access', 'Access your vault contents'),
('660e8400-e29b-41d4-a716-446655440003', 'Currency Exchange', 'Exchange Muggle money for Galleons'),
('660e8400-e29b-41d4-a716-446655440004', 'Loan Consultation', 'Discuss loan options and terms')
ON CONFLICT (name) DO NOTHING;

INSERT INTO branch_services (branch_id, service_id) VALUES
('550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440001'),
('550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440002'),
('550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440003'),
('550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440004'),
('550e8400-e29b-41d4-a716-446655440002', '660e8400-e29b-41d4-a716-446655440002'),
('550e8400-e29b-41d4-a716-446655440002', '660e8400-e29b-41d4-a716-446655440003'),
('550e8400-e29b-41d4-a716-446655440003', '660e8400-e29b-41d4-a716-446655440001'),
('550e8400-e29b-41d4-a716-446655440003', '660e8400-e29b-41d4-a716-446655440002'),
('550e8400-e29b-41d4-a716-446655440003', '660e8400-e29b-41d4-a716-446655440003')
ON CONFLICT (branch_id, service_id) DO NOTHING;

INSERT INTO admins (username, password_hash)
VALUES ('admin', '$2a$10$6U17cK7/nLQco3NM6hxcguPsgIGwOUcABqK5KcAflYBZ9M/eN4IOm')
ON CONFLICT (username) DO NOTHING;
