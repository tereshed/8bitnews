class TemplateValidationError(Exception):
    """Raised by studio.validate with a human-readable reason; the CLI turns
    this into a non-zero exit with the message printed to stderr."""
