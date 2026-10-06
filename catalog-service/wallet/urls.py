from django.urls import path

from .views import TopUpView, TransactionListView, WalletView

urlpatterns = [
    path("wallet", WalletView.as_view(), name="wallet"),
    path("wallet/transactions", TransactionListView.as_view(), name="wallet-transactions"),
    path("wallet/top-up", TopUpView.as_view(), name="wallet-top-up"),
]
