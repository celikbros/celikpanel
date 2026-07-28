-- A browser persists this opaque id before sending a package mutation. If the
-- response is lost, it can recover exactly that operation instead of guessing
-- from a recent operation with the same target.
-- Tarayıcı bu opak kimliği paket değişikliğini göndermeden önce saklar. Yanıt
-- kaybolursa aynı hedefteki yakın tarihli bir işlemi tahmin etmek yerine tam
-- olarak bu işleme yeniden bağlanabilir.
ALTER TABLE service_operations ADD COLUMN request_id TEXT;

CREATE UNIQUE INDEX idx_service_operations_request_id
    ON service_operations(request_id)
    WHERE request_id IS NOT NULL;
