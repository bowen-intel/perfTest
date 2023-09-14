#define _GNU_SOURCE
#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>
#include <sys/syscall.h>
#include <string.h>
#include <sys/ioctl.h>
#include <linux/perf_event.h>
#include <linux/hw_breakpoint.h>
#include <asm/unistd.h>
#include <errno.h>
#include <stdint.h>
#include <inttypes.h>

struct read_format {
  uint64_t nr;
  uint64_t time_enabled;
  uint64_t time_running;
  struct {
    uint64_t value;
    uint64_t id;
  } values[];
};

void do_something() {
  //int i;
  char* ptr;

  ptr = malloc(100*1024*1024);
  //for (i = 0; i < 100*1024*1024; i++) {
  //  ptr[i] = (char) (i & 0xff); // pagefault
  //}
  free(ptr);
}

int main(int argc, char* argv[]) {
  struct perf_event_attr pea;
  int fd1, fd2;
  uint64_t id1, id2;
  uint64_t val1, val2;
  char buf[4096];
  struct read_format* rf = (struct read_format*) buf;
  int i;

  memset(&pea, 0, sizeof(struct perf_event_attr));
  pea.type = PERF_TYPE_HARDWARE;
  pea.size = sizeof(struct perf_event_attr);
  pea.config = PERF_COUNT_HW_CPU_CYCLES;
  pea.disabled = 1;
  pea.exclude_kernel = 1;
  pea.exclude_hv = 1;
  pea.read_format = PERF_FORMAT_GROUP | PERF_FORMAT_ID|PERF_FORMAT_TOTAL_TIME_ENABLED|PERF_FORMAT_TOTAL_TIME_RUNNING;
  fd1 = syscall(__NR_perf_event_open, &pea, 0, -1, -1, 0);
  ioctl(fd1, PERF_EVENT_IOC_ID, &id1);

  memset(&pea, 0, sizeof(struct perf_event_attr));
  pea.type = PERF_TYPE_HARDWARE;
  pea.size = sizeof(struct perf_event_attr);
  pea.config = PERF_COUNT_HW_INSTRUCTIONS;
  pea.disabled = 1;
  pea.exclude_kernel = 1;
  pea.exclude_hv = 1;
  pea.read_format = PERF_FORMAT_GROUP | PERF_FORMAT_ID|PERF_FORMAT_TOTAL_TIME_ENABLED|PERF_FORMAT_TOTAL_TIME_RUNNING;
  fd2 = syscall(__NR_perf_event_open, &pea, 0, -1, fd1 /*!!!*/, 0);
  ioctl(fd2, PERF_EVENT_IOC_ID, &id2);


  ioctl(fd1, PERF_EVENT_IOC_RESET, PERF_IOC_FLAG_GROUP);
  ioctl(fd1, PERF_EVENT_IOC_ENABLE, PERF_IOC_FLAG_GROUP);
  do_something();
  ioctl(fd1, PERF_EVENT_IOC_DISABLE, PERF_IOC_FLAG_GROUP);


  read(fd1, buf, sizeof(buf));
  printf("read nr number of values: %"PRIu64"\n", rf->nr);
  printf("read time_enabled: %"PRIu64"\n", rf->time_enabled);
  printf("read time_running: %"PRIu64"\n", rf->time_running);
  for (i = 0; i < rf->nr; i++) {
    if (rf->values[i].id == id1) {
      val1 = rf->values[i].value;
    } else if (rf->values[i].id == id2) {
      val2 = rf->values[i].value;
    }
  }

  printf("cpu cycles: %"PRIu64"\n", val1);
  printf("instructions: %"PRIu64"\n", val2);

  return 0;
}
