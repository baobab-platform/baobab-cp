"""Drives baobab-erp's real provisioning event producer for the FB-04e convergence test.

It runs the ERP code that production runs: the provisioning command store (``advance``), the canonical event builder, the
transactional outbox and the signed dispatcher (``application/dispatch_worker.py``). It adds only what a test needs around them:
inserting a command row (ERP's accept path needs a whole approved-plan request, which this test is not about), reading the
authoritative state document, and inspecting or re-scheduling the outbox.

Usage (the ERP checkout's ``modules`` directory and the Shared checkout are on the environment, see convergence_test.go):

    erp_driver.py accept TENANT LEGAL_ENTITY[,LEGAL_ENTITY...]  -> {"operation_id": ..., "state": {...}}
    erp_driver.py advance OPERATION STATE [FAILURE_CODE]        -> the new state document, or null when nothing changed
    erp_driver.py state TENANT OPERATION                         -> the erp/v1 ProvisioningState the operation read answers
    erp_driver.py dispatch                                       -> runs one dispatcher pass (its JSON report is on stdout)
    erp_driver.py outbox OPERATION                               -> the outbox rows of the operation, by revision
    erp_driver.py due OPERATION                                  -> makes every retry of the operation due now
"""
import json
import sys
import uuid

import psycopg

from outbox.postgres_store import PostgresOutboxStore
from provisioning.command_events import EVENT_TYPE, provisioning_changed, state_document
from provisioning.command_store import PostgresProvisioningCommandStore

import os


def connect():
    return psycopg.connect(os.environ["DATABASE_URL"])


def accept(tenant: str, entities: list[str]) -> dict:
    operation = str(uuid.uuid4())
    with connect() as connection:
        store = PostgresProvisioningCommandStore(connection)
        with connection.cursor() as cursor:
            cursor.execute(
                """INSERT INTO baobab.erp_provisioning_command
                   (operation_id, tenant_id, idempotency_key, principal, request_fingerprint, tenant_provisioning_id,
                    plan_id, plan_version, plan_digest, legal_entity_ids)
                   VALUES (%s, %s, %s, 'convergence-test', %s, 'tp_convergence', 'plan-conv', 1, %s, %s)""",
                (operation, tenant, f"convergence-{operation}", "0" * 64, "sha256:" + "0" * 64, sorted(entities)))
        record = store.get(tenant, operation)
        # Revision 1, announced exactly as ``accept`` announces it, in the same transaction as the row.
        PostgresOutboxStore(connection).record_event(provisioning_changed(record))
        connection.commit()
        return {"operation_id": operation, "state": state_document(record)}


def advance(operation: str, state: str, failure: str | None) -> dict | None:
    with connect() as connection:
        record = PostgresProvisioningCommandStore(connection).advance(operation, state, failure)
        connection.commit()
        return state_document(record) if record else None


def read_state(tenant: str, operation: str) -> dict | None:
    with connect() as connection:
        record = PostgresProvisioningCommandStore(connection).get(tenant, operation)
        return state_document(record) if record else None


def outbox(operation: str) -> list[dict]:
    with connect() as connection, connection.cursor() as cursor:
        cursor.execute(
            """SELECT event_id::text, (payload_json->>'revision')::int, status, attempts, coalesce(last_error, '')
                 FROM baobab.event_outbox WHERE event_type = %s AND ce_subject = %s ORDER BY 2""",
            (EVENT_TYPE, f"provisioning:{operation}"))
        return [{"event_id": r[0], "revision": r[1], "status": r[2], "attempts": r[3], "last_error": r[4]}
                for r in cursor.fetchall()]


def due(operation: str) -> None:
    with connect() as connection, connection.cursor() as cursor:
        cursor.execute("UPDATE baobab.event_outbox SET next_attempt_at = now() - interval '1 second' "
                       "WHERE event_type = %s AND ce_subject = %s AND status = 'retry'", (EVENT_TYPE, f"provisioning:{operation}"))
        connection.commit()


def main(argv: list[str]) -> None:
    command, args = argv[1], argv[2:]
    if command == "accept":
        result = accept(args[0], args[1].split(","))
    elif command == "advance":
        result = advance(args[0], args[1], args[2] if len(args) > 2 else None)
    elif command == "state":
        result = read_state(args[0], args[1])
    elif command == "outbox":
        result = outbox(args[0])
    elif command == "due":
        due(args[0])
        result = None
    elif command == "dispatch":
        from application import dispatch_worker
        dispatch_worker.main()
        return
    else:
        raise SystemExit(f"unknown command {command}")
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main(sys.argv)
