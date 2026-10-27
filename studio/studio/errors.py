class TemplateValidationError(Exception):
    """Raised by studio.validate with a human-readable reason; the CLI turns
    this into a non-zero exit with the message printed to stderr."""


class BuildError(Exception):
    """Raised by studio.build with a human-readable reason (a script that
    fails its schema, an unimplemented --tts provider, a cover title that
    doesn't fit its template rect); the CLI turns this into a non-zero exit
    with the message printed to stderr."""
