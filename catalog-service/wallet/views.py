from rest_framework import generics, status
from rest_framework.response import Response
from rest_framework.views import APIView

from . import services
from .serializers import CoinTransactionSerializer, TopUpSerializer, WalletSerializer


class WalletView(APIView):
    def get(self, request):
        return Response(WalletSerializer(services.get_wallet(request.user)).data)


class TransactionListView(generics.ListAPIView):
    serializer_class = CoinTransactionSerializer

    def get_queryset(self):
        return services.get_wallet(self.request.user).transactions.all()


class TopUpView(APIView):
    """Stands in for the payment-gateway success callback; a real one would verify the gateway signature."""

    def post(self, request):
        serializer = TopUpSerializer(data=request.data)
        serializer.is_valid(raise_exception=True)
        txn, created = services.top_up(request.user, **serializer.validated_data)
        return Response(
            CoinTransactionSerializer(txn).data,
            status=status.HTTP_201_CREATED if created else status.HTTP_200_OK,
        )
