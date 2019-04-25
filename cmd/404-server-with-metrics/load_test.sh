START=$(date +%s)
for i in {1..100000}
do
  curl -s "http://localhost:8080/$i?[1-1000]"  &
  curl -d "hola" -s "http://localhost:8080/$i?[1-1000]" &
done
END=$(date +%s)
DIFF=$(( $END - $START ))
echo "It took $DIFF seconds"
