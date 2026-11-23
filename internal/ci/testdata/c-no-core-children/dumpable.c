/* THE DAMAGE CASE for TestCConformanceChildrenWriteNoCore: the same child,
   left dumpable, so every abort starts the host's crash handler. */
#include <sys/wait.h>
#include <unistd.h>

int refused( void )
{
    int status = 0;
    pid_t child = fork();
    if ( child == 0 )
    {
        abort();
        _exit( 0 );
    }
    waitpid( child, &status, 0 );
    return WIFSIGNALED( status );
}
