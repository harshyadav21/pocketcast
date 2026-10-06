from rest_framework import status
from rest_framework.exceptions import APIException


class InsufficientCoins(APIException):
    status_code = status.HTTP_402_PAYMENT_REQUIRED
    default_detail = "Not enough coins to unlock this episode."
    default_code = "insufficient_coins"


class EpisodeIsFree(APIException):
    status_code = status.HTTP_400_BAD_REQUEST
    default_detail = "This episode is free and does not need to be unlocked."
    default_code = "episode_is_free"


class DuplicatePaymentReference(APIException):
    status_code = status.HTTP_409_CONFLICT
    default_detail = "This payment reference was already used with a different amount."
    default_code = "duplicate_payment_reference"
