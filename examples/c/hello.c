// Query Hardcharger from C through the ADBC driver manager, which finds the
// driver by name (the hardcharger.toml manifest).
//
//   cc hello.c -I"$ADBC/include" -L"$ADBC/lib" -ladbc_driver_manager -o hello && ./hello
#include <arrow-adbc/adbc.h>
#include <arrow-adbc/adbc_driver_manager.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#define CHECK(call)                                             \
  do {                                                          \
    if ((call) != ADBC_STATUS_OK) {                             \
      fprintf(stderr, "%s failed: %s\n", #call, error.message); \
      if (error.release) error.release(&error);                 \
      return EXIT_FAILURE;                                      \
    }                                                           \
  } while (0)

int main(void) {
  struct AdbcError error = {0};
  struct AdbcDatabase database = {0};
  struct AdbcConnection connection = {0};
  struct AdbcStatement statement = {0};

  CHECK(AdbcDatabaseNew(&database, &error));
  // Search ADBC_DRIVER_PATH and the user and system directories for hardcharger.toml.
  CHECK(AdbcDriverManagerDatabaseSetLoadFlags(&database, ADBC_LOAD_FLAG_DEFAULT, &error));
  CHECK(AdbcDatabaseSetOption(&database, "driver", "hardcharger", &error));
  CHECK(AdbcDatabaseSetOption(&database, "uri", "hardcharger://localhost?tls=false", &error));
  CHECK(AdbcDatabaseSetOption(&database, "username", "hardcharger", &error));
  CHECK(AdbcDatabaseSetOption(&database, "password", "change-me", &error));
  CHECK(AdbcDatabaseSetOption(&database, "hardcharger.session.query_tag", "{\"app\": \"hello.c\"}",
                              &error));
  CHECK(AdbcDatabaseInit(&database, &error));
  CHECK(AdbcConnectionNew(&connection, &error));
  CHECK(AdbcConnectionInit(&connection, &database, &error));

  CHECK(AdbcStatementNew(&connection, &statement, &error));
  CHECK(AdbcStatementSetSqlQuery(&statement, "SELECT 42 AS answer", &error));
  struct ArrowArrayStream stream = {0};
  int64_t rows = 0;
  CHECK(AdbcStatementExecuteQuery(&statement, &stream, &rows, &error));
  struct ArrowArray batch = {0};
  while (stream.get_next(&stream, &batch) == 0 && batch.release != NULL) {
    const int32_t* answer = (const int32_t*)batch.children[0]->buffers[1];
    for (int64_t i = 0; i < batch.length; i++) printf("answer = %d\n", answer[i]);
    batch.release(&batch);
  }
  stream.release(&stream);

  CHECK(AdbcStatementRelease(&statement, &error));
  CHECK(AdbcConnectionRelease(&connection, &error));
  CHECK(AdbcDatabaseRelease(&database, &error));
  return EXIT_SUCCESS;
}
