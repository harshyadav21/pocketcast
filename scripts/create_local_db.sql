-- One-time setup for running catalog-service against a locally installed PostgreSQL.
-- Run as a superuser:  psql -h localhost -U postgres -f scripts/create_local_db.sql
-- Tables are created afterwards by Django migrations (python manage.py migrate).

CREATE ROLE pocketcast WITH LOGIN PASSWORD 'pocketcast' CREATEDB;  -- CREATEDB lets `manage.py test` create its test database
CREATE DATABASE pocketcast OWNER pocketcast;
