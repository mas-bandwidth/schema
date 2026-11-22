/* THE VALID CONTROL for TestCConformanceChildrenWriteNoCore: a child that is
   made not dumpable before it can abort. */
#include <sys/prctl.h>
#include <sys/wait.h>
#include <unistd.h>

int refused( void )
{
    int status = 0;
    pid_t child = fork();
    if ( child == 0 )
    {
#if defined( __linux__ )
        (void) prctl( PR_SET_DUMPABLE, 0, 0, 0, 0 );
#endif
        abort();
        _exit( 0 );
    }
    waitpid( child, &status, 0 );
    return WIFSIGNALED( status );
}
