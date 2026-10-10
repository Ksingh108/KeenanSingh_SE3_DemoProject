-- Branches table (3NF normalized)
CREATE TABLE IF NOT EXISTS branches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    address VARCHAR(500) NOT NULL,
    city VARCHAR(100) NOT NULL,
    phone VARCHAR(50) NOT NULL,
    hours VARCHAR(100) NOT NULL,
    capacity INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Services table (3NF normalized)
CREATE TABLE IF NOT EXISTS services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description VARCHAR(500),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Branch services junction table (many-to-many)
CREATE TABLE IF NOT EXISTS branch_services (
    branch_id UUID NOT NULL,
    service_id UUID NOT NULL,
    PRIMARY KEY (branch_id, service_id),
    FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);

-- Bookings table with proper foreign keys
CREATE TABLE IF NOT EXISTS bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id UUID NOT NULL,
    service_id UUID NOT NULL,
    customer_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    phone VARCHAR(50),
    date DATE NOT NULL,
    time_slot VARCHAR(50) NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE,
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE RESTRICT
);

-- Indexes for performance
CREATE INDEX idx_bookings_email ON bookings(email);
CREATE INDEX idx_bookings_date_time ON bookings(date, time_slot);
CREATE INDEX idx_bookings_branch_date ON bookings(branch_id, date);
CREATE INDEX idx_branches_city ON branches(city);

-- Insert sample branches
INSERT INTO branches (id, name, address, city, phone, hours, capacity) VALUES
('550e8400-e29b-41d4-a716-446655440001', 'Diagon Alley Main', '93 Diagon Alley', 'London', '+44 20 7946 0958', 'Mon-Fri 9:00-17:00, Sat 9:00-13:00', 15),
('550e8400-e29b-41d4-a716-446655440002', 'Knockturn Alley', '12 Knockturn Alley', 'London', '+44 20 7946 0959', 'Mon-Fri 10:00-16:00', 8),
('550e8400-e29b-41d4-a716-446655440003', 'Hogsmeade Village', '1 High Street', 'Hogsmeade', '+44 1436 123456', 'Mon-Sat 9:00-17:00', 12)
ON CONFLICT (id) DO NOTHING;

-- Insert sample services
INSERT INTO services (id, name, description) VALUES
('660e8400-e29b-41d4-a716-446655440001', 'Account Opening', 'Open new vault accounts'),
('660e8400-e29b-41d4-a716-446655440002', 'Vault Access', 'Access your vault contents'),
('660e8400-e29b-41d4-a716-446655440003', 'Currency Exchange', 'Exchange Muggle money for Galleons'),
('660e8400-e29b-41d4-a716-446655440004', 'Loan Consultation', 'Discuss loan options and terms')
ON CONFLICT (name) DO NOTHING;

-- Link services to branches
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

-- Admin table
CREATE TABLE IF NOT EXISTS admins (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Sessions table for persistent login. token_hash holds the hex SHA-256 digest of
-- the cookie's bearer token, never the token itself.
CREATE TABLE IF NOT EXISTS admin_sessions (
    token_hash VARCHAR(64) PRIMARY KEY,
    admin_id UUID NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (admin_id) REFERENCES admins(id) ON DELETE CASCADE
);

-- Index for cleanup of expired sessions
CREATE INDEX idx_sessions_expires_at ON admin_sessions(expires_at);

-- Append-only audit trail of privileged admin actions (who did what to which record).
-- admin_id is deliberately not a cascading foreign key: audit rows must outlive
-- the admin account they refer to, so removing an admin must never erase history.
CREATE TABLE IF NOT EXISTS admin_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id UUID NOT NULL REFERENCES admins(id) ON DELETE RESTRICT,
    action VARCHAR(50) NOT NULL,
    target_type VARCHAR(50) NOT NULL,
    target_id VARCHAR(100) NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_log_admin_id ON admin_audit_log(admin_id, created_at);
CREATE INDEX idx_audit_log_target ON admin_audit_log(target_type, target_id);

-- Insert default admin (password: admin123)
INSERT INTO admins (username, password_hash) 
VALUES ('admin', '$2a$10$6U17cK7/nLQco3NM6hxcguPsgIGwOUcABqK5KcAflYBZ9M/eN4IOm')
ON CONFLICT (username) DO NOTHING;