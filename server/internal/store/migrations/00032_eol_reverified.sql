-- +goose Up
-- Re-verificacion en linea (2026-09-26) del resto de la tabla de fin de vida. Windows: paginas de Microsoft Learn
-- (https://learn.microsoft.com/lifecycle/products/<producto>; las fechas de la pagina van un dia despues de la publicada,
-- p. ej. 10/15/2025 06:59 = 14/10/2025). Resultado: Windows 10 (Home y Pro: 1909, 2004, 20H2, 21H1, 21H2, 22H2) y
-- Windows Server 2008 R2, 2012, 2012 R2, 2016, 2019 y 2022 ya coincidian; Windows Server 2025 estaba mal (fin del
-- soporte extendido 2034-11-14, no 2034-10-10).
UPDATE os_eol SET eol_date = '2034-11-14', source = 'official' WHERE family = 'windows-server' AND cycle = '2025';

-- macOS: Apple no publica fechas de fin de soporte; endoflife.date las deriva de la ultima actualizacion de seguridad
-- publicada. Sonoma, Ventura y Monterey coincidian; Big Sur termino el 2026-02-02 (estaba 2023-09-26). Siguen como
-- 'estimate' (no es una fecha anunciada por el proveedor); 15, 26 y 27 no tienen fecha en esa fuente y se conservan las
-- estimadas (tres versiones mayores despues del lanzamiento).
UPDATE os_eol SET eol_date = '2026-02-02' WHERE family = 'macos' AND cycle = '11';

-- +goose Down
UPDATE os_eol SET eol_date = '2023-09-26' WHERE family = 'macos' AND cycle = '11';
UPDATE os_eol SET eol_date = '2034-10-10', source = 'official' WHERE family = 'windows-server' AND cycle = '2025';
