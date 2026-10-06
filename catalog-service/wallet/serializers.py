from rest_framework import serializers

from .models import CoinTransaction, Wallet


class WalletSerializer(serializers.ModelSerializer):
    class Meta:
        model = Wallet
        fields = ["balance", "updated_at"]


class CoinTransactionSerializer(serializers.ModelSerializer):
    class Meta:
        model = CoinTransaction
        fields = ["id", "type", "amount", "balance_after", "reference", "created_at"]


class TopUpSerializer(serializers.Serializer):
    amount = serializers.IntegerField(min_value=1, max_value=10000)
    payment_ref = serializers.CharField(max_length=100)
